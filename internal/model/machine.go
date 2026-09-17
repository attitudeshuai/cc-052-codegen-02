package model

import "time"

type MachineStatus string

const (
	MachineAvailable MachineStatus = "available"
	MachineBroken    MachineStatus = "broken"
)

type Machine struct {
	ID        int64         `db:"id" json:"id"`
	FarmID    int64         `db:"farm_id" json:"farm_id"`
	Name      string        `db:"name" json:"name"`
	Model     string        `db:"model" json:"model"`
	Status    MachineStatus `db:"status" json:"status"`
	CreatedAt time.Time     `db:"created_at" json:"created_at"`
}

type CreateMachineRequest struct {
	FarmID int64  `json:"farm_id" binding:"required"`
	Name   string `json:"name" binding:"required"`
	Model  string `json:"model" binding:"required"`
}

// UtilizationSlot 一天里的一段占用
type UtilizationSlot struct {
	ReservationID int64             `json:"reservation_id"`
	PlotID        int64             `json:"plot_id"`
	Operation     string            `json:"operation"`
	StartAt       time.Time         `json:"start_at"`
	EndAt         time.Time         `json:"end_at"`
	Status        ReservationStatus `json:"status"`
	Minutes       int               `json:"minutes"`
}

// MachineUtilization 一台机器某天的忙闲统计
type MachineUtilization struct {
	MachineID   int64             `json:"machine_id"`
	MachineName string            `json:"machine_name"`
	Model       string            `json:"model"`
	Status      MachineStatus     `json:"status"`
	Date        string            `json:"date"`
	BusyMinutes int               `json:"busy_minutes"`
	IdleMinutes int               `json:"idle_minutes"`
	Slots       []UtilizationSlot `json:"slots"`
}

// BreakdownResult 报坏改派的结果
type BreakdownResult struct {
	Machine    *Machine             `json:"machine"`
	Reassigned []MachineReservation `json:"reassigned"` // 已改派出去的预约（含新机器）
	Unassigned []MachineReservation `json:"unassigned"` // 没有空闲同型号机器、仍挂在坏机器上的预约
}
