package repository

import (
	"cc-052/internal/model"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// 预约/改派事务内发现的领域错误，service 层据此映射 HTTP 响应
var (
	ErrMachineUnavailable  = errors.New("machine is not available")
	ErrInvalidBookingState = errors.New("booking state does not allow this operation")
)

type MachineRepo struct {
	db *sqlx.DB
}

func NewMachineRepo(db *sqlx.DB) *MachineRepo {
	return &MachineRepo{db: db}
}

func (r *MachineRepo) Create(m *model.Machine) error {
	query := `INSERT INTO machine (farm_id, name, model, kind, status)
	          VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`
	return r.db.QueryRow(query, m.FarmID, m.Name, m.Model, m.Kind, m.Status).
		Scan(&m.ID, &m.CreatedAt)
}

func (r *MachineRepo) GetByID(id int64) (*model.Machine, error) {
	var m model.Machine
	query := `SELECT id, farm_id, name, model, kind, status, created_at FROM machine WHERE id = $1`
	if err := r.db.Get(&m, query, id); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *MachineRepo) ListByFarm(farmID int64) ([]model.Machine, error) {
	var machines []model.Machine
	query := `SELECT id, farm_id, name, model, kind, status, created_at FROM machine WHERE farm_id = $1 ORDER BY id`
	if err := r.db.Select(&machines, query, farmID); err != nil {
		return nil, err
	}
	return machines, nil
}

func (r *MachineRepo) UpdateStatus(id int64, status model.MachineStatus) error {
	query := `UPDATE machine SET status = $1 WHERE id = $2`
	_, err := r.db.Exec(query, status, id)
	return err
}

// ListAvailableSameModel 故障改派候选：同农场、同型号、可用，按 id 排序保证结果确定
func (r *MachineRepo) ListAvailableSameModel(farmID int64, machineModel string, excludeID int64) ([]model.Machine, error) {
	var machines []model.Machine
	query := `SELECT id, farm_id, name, model, kind, status, created_at FROM machine
	          WHERE farm_id = $1 AND model = $2 AND status = 'available' AND id <> $3
	          ORDER BY id`
	if err := r.db.Select(&machines, query, farmID, machineModel, excludeID); err != nil {
		return nil, err
	}
	return machines, nil
}

type BookingRepo struct {
	db *sqlx.DB
}

func NewBookingRepo(db *sqlx.DB) *BookingRepo {
	return &BookingRepo{db: db}
}

const bookingColumns = `id, machine_id, plot_id, work_step, step_seq, start_at, end_at, status, note, created_at`

// findConflictsTx 查同一台机器时段重叠的未取消预约（半开区间 [start, end) 重叠判断）
func findConflictsTx(tx *sqlx.Tx, machineID int64, start, end time.Time, excludeID int64) ([]model.Booking, error) {
	var conflicts []model.Booking
	query := `SELECT ` + bookingColumns + ` FROM booking
	          WHERE machine_id = $1 AND status <> 'cancelled'
	            AND start_at < $2 AND end_at > $3 AND id <> $4
	          ORDER BY start_at`
	if err := tx.Select(&conflicts, query, machineID, end, start, excludeID); err != nil {
		return nil, err
	}
	return conflicts, nil
}

// CreateTx 事务化创建预约：锁机器行（串行化同一台机器的预约写入）→ 复查机器可用 → 复查时段冲突 → 插入。
// 返回非空 conflicts 表示时段被占；调用方据此向用户指出撞上了哪条预约。
func (r *BookingRepo) CreateTx(b *model.Booking) (conflicts []model.Booking, err error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var machineStatus string
	if err := tx.Get(&machineStatus, `SELECT status FROM machine WHERE id = $1 FOR UPDATE`, b.MachineID); err != nil {
		return nil, err // sql.ErrNoRows: 机器不存在
	}
	if machineStatus != string(model.MachineAvailable) {
		return nil, ErrMachineUnavailable
	}

	conflicts, err = findConflictsTx(tx, b.MachineID, b.StartAt, b.EndAt, 0)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}

	insert := `INSERT INTO booking (machine_id, plot_id, work_step, step_seq, start_at, end_at, status, note)
	           VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`
	if err := tx.QueryRow(insert, b.MachineID, b.PlotID, b.WorkStep, b.StepSeq,
		b.StartAt, b.EndAt, b.Status, b.Note).Scan(&b.ID, &b.CreatedAt); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

func (r *BookingRepo) GetByID(id int64) (*model.Booking, error) {
	var b model.Booking
	query := `SELECT ` + bookingColumns + ` FROM booking WHERE id = $1`
	if err := r.db.Get(&b, query, id); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BookingRepo) UpdateStatus(id int64, status model.BookingStatus) error {
	query := `UPDATE booking SET status = $1 WHERE id = $2`
	_, err := r.db.Exec(query, status, id)
	return err
}

// ReassignTx 事务化改派：锁预约行与目标机器行 → 复查预约可改派、机器可用、时段不冲突 → 换机器（可顺带改时段）。
func (r *BookingRepo) ReassignTx(bookingID, targetMachineID int64, start, end time.Time) (conflicts []model.Booking, err error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var b model.Booking
	if err := tx.Get(&b, `SELECT `+bookingColumns+` FROM booking WHERE id = $1 FOR UPDATE`, bookingID); err != nil {
		return nil, err // sql.ErrNoRows: 预约不存在
	}
	if b.Status != model.BookingScheduled && b.Status != model.BookingInProgress {
		return nil, ErrInvalidBookingState
	}

	var machineStatus string
	if err := tx.Get(&machineStatus, `SELECT status FROM machine WHERE id = $1 FOR UPDATE`, targetMachineID); err != nil {
		return nil, err
	}
	if machineStatus != string(model.MachineAvailable) {
		return nil, ErrMachineUnavailable
	}

	conflicts, err = findConflictsTx(tx, targetMachineID, start, end, bookingID)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}

	update := `UPDATE booking SET machine_id = $1, start_at = $2, end_at = $3 WHERE id = $4`
	if _, err := tx.Exec(update, targetMachineID, start, end, bookingID); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// ListByPlot 地块作业步骤（未取消），按 step_seq、开始时间排序，带出机器名
func (r *BookingRepo) ListByPlot(plotID int64) ([]model.PlotStepView, error) {
	steps := []model.PlotStepView{}
	query := `SELECT b.id, b.machine_id, b.plot_id, b.work_step, b.step_seq, b.start_at, b.end_at,
	                 b.status, b.note, b.created_at, m.name AS machine_name
	          FROM booking b JOIN machine m ON m.id = b.machine_id
	          WHERE b.plot_id = $1 AND b.status <> 'cancelled'
	          ORDER BY b.step_seq, b.start_at`
	if err := r.db.Select(&steps, query, plotID); err != nil {
		return nil, err
	}
	return steps, nil
}

// ListByMachineRange 机器在 [from, to) 内重叠的未取消预约，用于排班与忙闲统计
func (r *BookingRepo) ListByMachineRange(machineID int64, from, to time.Time) ([]model.Booking, error) {
	bookings := []model.Booking{}
	query := `SELECT ` + bookingColumns + ` FROM booking
	          WHERE machine_id = $1 AND status <> 'cancelled'
	            AND start_at < $2 AND end_at > $3
	          ORDER BY start_at`
	if err := r.db.Select(&bookings, query, machineID, to, from); err != nil {
		return nil, err
	}
	return bookings, nil
}

// ListActiveByMachine 机器剩余未完成的预约（故障后批量改派用）
func (r *BookingRepo) ListActiveByMachine(machineID int64) ([]model.Booking, error) {
	bookings := []model.Booking{}
	query := `SELECT ` + bookingColumns + ` FROM booking
	          WHERE machine_id = $1 AND status IN ('scheduled', 'in_progress')
	          ORDER BY start_at`
	if err := r.db.Select(&bookings, query, machineID); err != nil {
		return nil, err
	}
	return bookings, nil
}

// MaxStepSeq 地块当前最大的作业序号（自动排序用）
func (r *BookingRepo) MaxStepSeq(plotID int64) (int, error) {
	var seq int
	query := `SELECT COALESCE(MAX(step_seq), 0) FROM booking WHERE plot_id = $1 AND status <> 'cancelled'`
	if err := r.db.Get(&seq, query, plotID); err != nil {
		return 0, err
	}
	return seq, nil
}
