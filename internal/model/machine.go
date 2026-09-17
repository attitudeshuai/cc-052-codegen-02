package model

import "time"

// Machine lifecycle: available <-> maintenance <-> broken（报障后不再接受新预约）
type MachineStatus string

const (
	MachineAvailable   MachineStatus = "available"
	MachineMaintenance MachineStatus = "maintenance"
	MachineBroken      MachineStatus = "broken"
)

type Machine struct {
	ID        int64         `db:"id" json:"id"`
	FarmID    int64         `db:"farm_id" json:"farm_id"`
	Name      string        `db:"name" json:"name"`
	Model     string        `db:"model" json:"model"`
	Kind      string        `db:"kind" json:"kind"`
	Status    MachineStatus `db:"status" json:"status"`
	CreatedAt time.Time     `db:"created_at" json:"created_at"`
}

type CreateMachineRequest struct {
	FarmID int64  `json:"farm_id" binding:"required"`
	Name   string `json:"name" binding:"required"`
	Model  string `json:"model" binding:"required"`
	Kind   string `json:"kind"`
}

type UpdateMachineStatusRequest struct {
	Status MachineStatus `json:"status" binding:"required"`
}

// Booking lifecycle: scheduled -> in_progress -> done；scheduled/in_progress -> cancelled
type BookingStatus string

const (
	BookingScheduled  BookingStatus = "scheduled"
	BookingInProgress BookingStatus = "in_progress"
	BookingDone       BookingStatus = "done"
	BookingCancelled  BookingStatus = "cancelled"
)

type Booking struct {
	ID        int64         `db:"id" json:"id"`
	MachineID int64         `db:"machine_id" json:"machine_id"`
	PlotID    int64         `db:"plot_id" json:"plot_id"`
	WorkStep  string        `db:"work_step" json:"work_step"`
	StepSeq   int           `db:"step_seq" json:"step_seq"`
	StartAt   time.Time     `db:"start_at" json:"start_at"`
	EndAt     time.Time     `db:"end_at" json:"end_at"`
	Status    BookingStatus `db:"status" json:"status"`
	Note      string        `db:"note" json:"note"`
	CreatedAt time.Time     `db:"created_at" json:"created_at"`
}

type CreateBookingRequest struct {
	MachineID int64  `json:"machine_id" binding:"required"`
	PlotID    int64  `json:"plot_id" binding:"required"`
	WorkStep  string `json:"work_step" binding:"required"`
	StepSeq   int    `json:"step_seq"`                    // 0 = 自动排在该地块现有作业之后
	StartAt   string `json:"start_at" binding:"required"` // RFC3339
	EndAt     string `json:"end_at" binding:"required"`
	Note      string `json:"note"`
}

type UpdateBookingStatusRequest struct {
	Status BookingStatus `json:"status" binding:"required"`
}

type ReassignBookingRequest struct {
	MachineID *int64 `json:"machine_id"` // 空 = 自动挑一台同型号且该时段空闲的机器
	StartAt   string `json:"start_at"`   // 可选：同时改时段（与 end_at 成对出现），改派后不得打乱地块作业顺序
	EndAt     string `json:"end_at"`
}

// PlotStepView 地块作业进度里的一步（带上机器信息）
type PlotStepView struct {
	Booking
	MachineName string `db:"machine_name" json:"machine_name"`
}

type PlotWorkProgress struct {
	PlotID      int64          `json:"plot_id"`
	PlotName    string         `json:"plot_name"`
	TotalSteps  int            `json:"total_steps"`
	DoneSteps   int            `json:"done_steps"`
	CurrentStep *PlotStepView  `json:"current_step,omitempty"`
	Steps       []PlotStepView `json:"steps"`
}

// MachineDailyStats 一台机器某一天的忙闲汇总（按 UTC 自然日）
type MachineDailyStats struct {
	MachineID    int64     `json:"machine_id"`
	MachineName  string    `json:"machine_name"`
	Date         string    `json:"date"`
	BusyMinutes  int       `json:"busy_minutes"`
	IdleMinutes  int       `json:"idle_minutes"`
	BookingCount int       `json:"booking_count"`
	Bookings     []Booking `json:"bookings"`
}

// ReassignResult 批量改派时每条预约的结果
type ReassignResult struct {
	BookingID    int64  `json:"booking_id"`
	OK           bool   `json:"ok"`
	NewMachineID int64  `json:"new_machine_id,omitempty"`
	Error        string `json:"error,omitempty"`
}
