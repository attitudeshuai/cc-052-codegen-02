package handler

import (
	"cc-052/internal/model"
	"cc-052/internal/service"
	"cc-052/pkg/response"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type MachineHandler struct {
	svc *service.MachineService
}

func NewMachineHandler(svc *service.MachineService) *MachineHandler {
	return &MachineHandler{svc: svc}
}

// writeMachineError 统一把 service 层错误映射成 HTTP 响应
func writeMachineError(c *gin.Context, err error) {
	var ve *service.ValidationError
	var ce *service.BookingConflictError
	switch {
	case errors.As(err, &ce):
		// 指出跟哪条预约撞了
		response.Conflict(c, ce.Error(), gin.H{"conflicts": ce.Conflicts})
	case errors.As(err, &ve):
		response.BadRequest(c, ve.Error())
	case errors.Is(err, service.ErrMachineNotFound),
		errors.Is(err, service.ErrBookingNotFound),
		errors.Is(err, service.ErrPlotNotFound),
		errors.Is(err, service.ErrFarmNotFound):
		response.NotFound(c, err.Error())
	default:
		response.InternalError(c, err.Error())
	}
}

func (h *MachineHandler) CreateMachine(c *gin.Context) {
	var req model.CreateMachineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	m, err := h.svc.CreateMachine(&req)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Created(c, m)
}

func (h *MachineHandler) GetMachine(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	m, err := h.svc.GetMachine(id)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, m)
}

func (h *MachineHandler) ListMachines(c *gin.Context) {
	farmID, err := strconv.ParseInt(c.Query("farm_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid farm_id")
		return
	}
	machines, err := h.svc.ListMachines(farmID)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, machines)
}

// UpdateMachineStatus 报障/维修/恢复：POST /machines/:id/status {"status":"broken"}
func (h *MachineHandler) UpdateMachineStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req model.UpdateMachineStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	m, err := h.svc.UpdateMachineStatus(id, req.Status)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, m)
}

// ReassignRemaining 机器坏了，把它剩余的预约全部改派给同型号空闲机器
func (h *MachineHandler) ReassignRemaining(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	results, err := h.svc.ReassignRemaining(id)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, gin.H{"results": results})
}

// MachineDailyStats 一台机器一天忙了多久、空着多久：GET /machines/:id/daily-stats?date=2026-09-17
func (h *MachineHandler) MachineDailyStats(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	date := c.Query("date")
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	stats, err := h.svc.MachineDailyStats(id, date)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, stats)
}

// ListMachineBookings 机器排班表：GET /machines/:id/bookings?from=&to=
func (h *MachineHandler) ListMachineBookings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	bookings, err := h.svc.ListMachineBookings(id, c.Query("from"), c.Query("to"))
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, bookings)
}

// CreateBooking 按时段预约；撞单返回 409 并带上撞上的预约
func (h *MachineHandler) CreateBooking(c *gin.Context) {
	var req model.CreateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	b, err := h.svc.CreateBooking(&req)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Created(c, b)
}

func (h *MachineHandler) GetBooking(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	b, err := h.svc.GetBooking(id)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, b)
}

// UpdateBookingStatus 开工/完工/取消：POST /bookings/:id/status {"status":"in_progress"}
func (h *MachineHandler) UpdateBookingStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req model.UpdateBookingStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	b, err := h.svc.UpdateBookingStatus(id, req.Status)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, b)
}

// ReassignBooking 改派：POST /bookings/:id/reassign {"machine_id":2} 或空 body 自动挑选
func (h *MachineHandler) ReassignBooking(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var req model.ReassignBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		// 允许空 body：表示自动挑选同型号空闲机器
		response.BadRequest(c, err.Error())
		return
	}
	b, err := h.svc.ReassignBooking(id, &req)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, b)
}

// PlotWorkProgress 这块地的作业做到哪一步：GET /plots/:id/work-progress
func (h *MachineHandler) PlotWorkProgress(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	progress, err := h.svc.PlotWorkProgress(id)
	if err != nil {
		writeMachineError(c, err)
		return
	}
	response.Success(c, progress)
}
