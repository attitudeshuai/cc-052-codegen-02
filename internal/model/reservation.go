package model

import "time"

type ReservationStatus string

const (
	ReservationScheduled  ReservationStatus = "scheduled"
	ReservationInProgress ReservationStatus = "in_progress"
	ReservationDone       ReservationStatus = "done"
	ReservationCancelled  ReservationStatus = "cancelled"
)

type MachineReservation struct {
	ID             int64             `db:"id" json:"id"`
	MachineID      int64             `db:"machine_id" json:"machine_id"`
	PlotID         int64             `db:"plot_id" json:"plot_id"`
	Operation      string            `db:"operation" json:"operation"`
	Seq            int               `db:"seq" json:"seq"`
	StartAt        time.Time         `db:"start_at" json:"start_at"`
	EndAt          time.Time         `db:"end_at" json:"end_at"`
	Status         ReservationStatus `db:"status" json:"status"`
	ReassignedFrom *int64            `db:"reassigned_from" json:"reassigned_from,omitempty"`
	CreatedAt      time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time         `db:"updated_at" json:"updated_at"`
}

type CreateReservationRequest struct {
	MachineID int64  `json:"machine_id" binding:"required"`
	PlotID    int64  `json:"plot_id" binding:"required"`
	Operation string `json:"operation" binding:"required"`
	StartAt   string `json:"start_at" binding:"required"` // RFC3339
	EndAt     string `json:"end_at" binding:"required"`   // RFC3339
}

// PlotWorkProgress 一块地的作业进度：做到哪一步了
type PlotWorkProgress struct {
	PlotID      int64                `json:"plot_id"`
	PlotName    string               `json:"plot_name"`
	TotalSteps  int                  `json:"total_steps"`
	DoneSteps   int                  `json:"done_steps"`
	CurrentStep *MachineReservation  `json:"current_step,omitempty"` // 第一个未完成的步骤
	Steps       []MachineReservation `json:"steps"`                  // 按 seq 升序
}
