package repository

import (
	"cc-052/internal/model"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type ReservationRepo struct {
	db *sqlx.DB
}

func NewReservationRepo(db *sqlx.DB) *ReservationRepo {
	return &ReservationRepo{db: db}
}

const reservationCols = `id, machine_id, plot_id, operation, seq, start_at, end_at, status, reassigned_from, created_at, updated_at`

// CreateIfNoConflict 在单事务内完成：锁机器 -> 校验机器可用 -> 查重叠 -> 锁地块定 seq -> 插入。
// 有重叠时返回冲突的预约列表（不写库）；机器故障返回 model.ErrMachineBroken。
func (r *ReservationRepo) CreateIfNoConflict(res *model.MachineReservation) ([]model.MachineReservation, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 锁机器行：串行化同一台机器的预约，同时读到最新状态
	var m model.Machine
	if err := tx.Get(&m, `SELECT id, status FROM machine WHERE id = $1 FOR UPDATE`, res.MachineID); err != nil {
		return nil, err
	}
	if m.Status == model.MachineBroken {
		return nil, model.ErrMachineBroken
	}

	// 重叠判定：existing.start < new.end AND existing.end > new.start（首尾相接不算冲突）
	var conflicts []model.MachineReservation
	if err := tx.Select(&conflicts,
		`SELECT `+reservationCols+` FROM machine_reservation
		 WHERE machine_id = $1 AND status <> 'cancelled' AND start_at < $2 AND end_at > $3
		 ORDER BY start_at`, res.MachineID, res.EndAt, res.StartAt); err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}

	// 锁地块行：串行化同一地块的 seq 分配
	var plotID int64
	if err := tx.Get(&plotID, `SELECT id FROM plot WHERE id = $1 FOR UPDATE`, res.PlotID); err != nil {
		return nil, err
	}

	if err := tx.Get(&res.Seq,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM machine_reservation WHERE plot_id = $1 AND status <> 'cancelled'`,
		res.PlotID); err != nil {
		return nil, err
	}

	err = tx.QueryRow(
		`INSERT INTO machine_reservation (machine_id, plot_id, operation, seq, start_at, end_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, status, created_at, updated_at`,
		res.MachineID, res.PlotID, res.Operation, res.Seq, res.StartAt, res.EndAt).
		Scan(&res.ID, &res.Status, &res.CreatedAt, &res.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (r *ReservationRepo) GetByID(id int64) (*model.MachineReservation, error) {
	var res model.MachineReservation
	if err := r.db.Get(&res, `SELECT `+reservationCols+` FROM machine_reservation WHERE id = $1`, id); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListActiveByPlot 一块地上未取消的预约，按作业顺序排列
func (r *ReservationRepo) ListActiveByPlot(plotID int64) ([]model.MachineReservation, error) {
	var list []model.MachineReservation
	if err := r.db.Select(&list,
		`SELECT `+reservationCols+` FROM machine_reservation WHERE plot_id = $1 AND status <> 'cancelled' ORDER BY seq`,
		plotID); err != nil {
		return nil, err
	}
	return list, nil
}

// ListByMachineAndRange 一台机器与 [from, to) 有重叠的未取消预约
func (r *ReservationRepo) ListByMachineAndRange(machineID int64, from, to time.Time) ([]model.MachineReservation, error) {
	var list []model.MachineReservation
	if err := r.db.Select(&list,
		`SELECT `+reservationCols+` FROM machine_reservation
		 WHERE machine_id = $1 AND status <> 'cancelled' AND start_at < $2 AND end_at > $3
		 ORDER BY start_at`, machineID, to, from); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *ReservationRepo) List(machineID, plotID *int64) ([]model.MachineReservation, error) {
	var list []model.MachineReservation
	query := `SELECT ` + reservationCols + ` FROM machine_reservation
		 WHERE ($1::bigint IS NULL OR machine_id = $1)
		   AND ($2::bigint IS NULL OR plot_id = $2)
		 ORDER BY start_at DESC LIMIT 200`
	if err := r.db.Select(&list, query, machineID, plotID); err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateStatus 状态流转，仅当当前状态在 from 列表中才更新；否则返回 model.ErrInvalidTransition
func (r *ReservationRepo) UpdateStatus(id int64, to model.ReservationStatus, from ...model.ReservationStatus) error {
	arr := make([]string, len(from))
	for i, s := range from {
		arr[i] = string(s)
	}
	res, err := r.db.Exec(
		`UPDATE machine_reservation SET status = $1, updated_at = NOW() WHERE id = $2 AND status = ANY($3)`,
		to, id, pq.Array(arr))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// 区分「不存在」与「状态不对」
		if _, err := r.GetByID(id); err != nil {
			return err
		}
		return model.ErrInvalidTransition
	}
	return nil
}

// HasEarlierUnfinished 同一地块上是否还有 seq 更小且未完成的步骤（用于开工顺序校验）
func (r *ReservationRepo) HasEarlierUnfinished(plotID int64, seq int) (bool, error) {
	var exists bool
	err := r.db.Get(&exists,
		`SELECT EXISTS(SELECT 1 FROM machine_reservation
		 WHERE plot_id = $1 AND seq < $2 AND status NOT IN ('done','cancelled'))`,
		plotID, seq)
	return exists, err
}
