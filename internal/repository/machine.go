package repository

import (
	"cc-052/internal/model"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type MachineRepo struct {
	db *sqlx.DB
}

func NewMachineRepo(db *sqlx.DB) *MachineRepo {
	return &MachineRepo{db: db}
}

func (r *MachineRepo) Create(m *model.Machine) error {
	query := `INSERT INTO machine (farm_id, name, model)
	          VALUES ($1, $2, $3) RETURNING id, status, created_at`
	return r.db.QueryRow(query, m.FarmID, m.Name, m.Model).
		Scan(&m.ID, &m.Status, &m.CreatedAt)
}

func (r *MachineRepo) GetByID(id int64) (*model.Machine, error) {
	var m model.Machine
	query := `SELECT id, farm_id, name, model, status, created_at FROM machine WHERE id = $1`
	if err := r.db.Get(&m, query, id); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *MachineRepo) ListByFarm(farmID int64) ([]model.Machine, error) {
	var machines []model.Machine
	query := `SELECT id, farm_id, name, model, status, created_at FROM machine WHERE farm_id = $1 ORDER BY id`
	if err := r.db.Select(&machines, query, farmID); err != nil {
		return nil, err
	}
	return machines, nil
}

// Repair 修复机器：broken -> available
func (r *MachineRepo) Repair(id int64) (*model.Machine, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	m, err := lockMachine(tx, id)
	if err != nil {
		return nil, err
	}
	if m.Status != model.MachineBroken {
		return nil, model.ErrMachineNotBroken
	}
	if _, err := tx.Exec(`UPDATE machine SET status = $1 WHERE id = $2`, model.MachineAvailable, id); err != nil {
		return nil, err
	}
	m.Status = model.MachineAvailable
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

// ReassignPlanner 纯函数：为待改派的预约挑选新机器。
// pending 按开始时间升序；busy 是每台候选机器已有的占用时段。
// 返回 reservationID -> newMachineID，未分配的预约不在 map 中。
type ReassignPlanner func(pending []model.MachineReservation, candidates []model.Machine, busy map[int64][]model.MachineReservation) map[int64]int64

// BreakdownAndReassign 报坏并改派：机器置为 broken，把未完成的预约改派给同型号且空闲的机器。
// 整个流程在一个事务里；所有机器行按 id 升序一次性加锁，避免与并发改派死锁。
// planner 由 service 注入，保持纯函数可测。
func (r *MachineRepo) BreakdownAndReassign(machineID int64, planner ReassignPlanner) (*model.Machine, []model.MachineReservation, []model.MachineReservation, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()

	// 普通读取确定候选集合（最终以加锁后的状态为准）
	var broken model.Machine
	if err := tx.Get(&broken,
		`SELECT id, farm_id, name, model, status, created_at FROM machine WHERE id = $1`, machineID); err != nil {
		return nil, nil, nil, err
	}

	var candidateIDs []int64
	if err := tx.Select(&candidateIDs,
		`SELECT id FROM machine WHERE farm_id = $1 AND model = $2 AND id <> $3 AND status = $4 ORDER BY id`,
		broken.FarmID, broken.Model, broken.ID, model.MachineAvailable); err != nil {
		return nil, nil, nil, err
	}

	// 按 id 升序一次性锁住坏机器 + 所有候选机器
	lockIDs := append([]int64{machineID}, candidateIDs...)
	var locked []model.Machine
	if err := tx.Select(&locked,
		`SELECT id, farm_id, name, model, status, created_at FROM machine WHERE id = ANY($1) ORDER BY id FOR UPDATE`,
		pq.Array(lockIDs)); err != nil {
		return nil, nil, nil, err
	}

	// 加锁后复核状态（等待锁期间可能已被别的并发事务改掉）
	var candidates []model.Machine
	for _, m := range locked {
		if m.ID == machineID {
			broken = m
			continue
		}
		if m.Status == model.MachineAvailable {
			candidates = append(candidates, m)
		}
	}
	if broken.Status == model.MachineBroken {
		return nil, nil, nil, model.ErrMachineBroken
	}

	if _, err := tx.Exec(`UPDATE machine SET status = $1 WHERE id = $2`, model.MachineBroken, machineID); err != nil {
		return nil, nil, nil, err
	}
	broken.Status = model.MachineBroken

	// 待改派：该机器所有未完成的预约，按开始时间排序
	var pending []model.MachineReservation
	if err := tx.Select(&pending,
		`SELECT id, machine_id, plot_id, operation, seq, start_at, end_at, status, reassigned_from, created_at, updated_at
		 FROM machine_reservation
		 WHERE machine_id = $1 AND status IN ('scheduled','in_progress')
		 ORDER BY start_at, id`, machineID); err != nil {
		return nil, nil, nil, err
	}

	var reassigned, unassigned []model.MachineReservation
	if len(pending) > 0 && len(candidates) > 0 {
		// 候选机器已有的占用时段（只取与待改派时段可能重叠的）
		candIDs := make([]int64, len(candidates))
		for i, c := range candidates {
			candIDs[i] = c.ID
		}
		busy := map[int64][]model.MachineReservation{}
		var busyRows []model.MachineReservation
		if err := tx.Select(&busyRows,
			`SELECT id, machine_id, plot_id, operation, seq, start_at, end_at, status, reassigned_from, created_at, updated_at
			 FROM machine_reservation
			 WHERE machine_id = ANY($1) AND status <> 'cancelled' AND end_at > $2
			 ORDER BY start_at, id`, pq.Array(candIDs), pending[0].StartAt); err != nil {
			return nil, nil, nil, err
		}
		for _, b := range busyRows {
			busy[b.MachineID] = append(busy[b.MachineID], b)
		}

		assignments := planner(pending, candidates, busy)
		for _, p := range pending {
			newMachineID, ok := assignments[p.ID]
			if !ok {
				unassigned = append(unassigned, p)
				continue
			}
			if _, err := tx.Exec(
				`UPDATE machine_reservation SET machine_id = $1, reassigned_from = $2, updated_at = NOW() WHERE id = $3`,
				newMachineID, machineID, p.ID); err != nil {
				return nil, nil, nil, err
			}
			p.MachineID = newMachineID
			p.ReassignedFrom = &machineID
			reassigned = append(reassigned, p)
		}
	} else {
		unassigned = pending
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}
	return &broken, reassigned, unassigned, nil
}

func lockMachine(tx *sqlx.Tx, id int64) (*model.Machine, error) {
	var m model.Machine
	if err := tx.Get(&m,
		`SELECT id, farm_id, name, model, status, created_at FROM machine WHERE id = $1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	return &m, nil
}
