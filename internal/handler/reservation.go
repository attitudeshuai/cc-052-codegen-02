package handler

import (
	"cc-052/internal/model"
	"cc-052/internal/service"
	"cc-052/pkg/response"
	"database/sql"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

type ReservationHandler struct {
	svc *service.ReservationService
}

func NewReservationHandler(svc *service.ReservationService) *ReservationHandler {
	return &ReservationHandler{svc: svc}
}

// Create 按时段预约；同一台机器时段重叠返回 409 并指出撞上的预约
func (h *ReservationHandler) Create(c *gin.Context) {
	var req model.CreateReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	res, err := h.svc.Create(&req)
	if err != nil {
		var conflictErr *service.ConflictError
		switch {
		case errors.As(err, &conflictErr):
			response.Conflict(c, conflictErr.Error(), gin.H{"conflicts": conflictErr.Conflicts})
		case errors.Is(err, model.ErrMachineBroken):
			response.Conflict(c, "机器故障中，不能预约；请先报修或改派", nil)
		case errors.Is(err, sql.ErrNoRows):
			response.NotFound(c, "machine or plot not found")
		default:
			response.BadRequest(c, err.Error())
		}
		return
	}
	response.Created(c, res)
}

func (h *ReservationHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	res, err := h.svc.GetByID(id)
	if err != nil {
		response.NotFound(c, "reservation not found")
		return
	}
	response.Success(c, res)
}

// List 按机器/地块筛选：GET /reservations?machine_id=1&plot_id=2
func (h *ReservationHandler) List(c *gin.Context) {
	var machineID, plotID *int64
	if v := c.Query("machine_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.BadRequest(c, "invalid machine_id")
			return
		}
		machineID = &id
	}
	if v := c.Query("plot_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.BadRequest(c, "invalid plot_id")
			return
		}
		plotID = &id
	}
	list, err := h.svc.List(machineID, plotID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, list)
}

// Start 开工：前置步骤未完成或机器故障会被拒绝
func (h *ReservationHandler) Start(c *gin.Context) {
	h.transition(c, h.svc.Start)
}

// Complete 完工
func (h *ReservationHandler) Complete(c *gin.Context) {
	h.transition(c, h.svc.Complete)
}

// Cancel 取消
func (h *ReservationHandler) Cancel(c *gin.Context) {
	h.transition(c, h.svc.Cancel)
}

func (h *ReservationHandler) transition(c *gin.Context, fn func(int64) (*model.MachineReservation, error)) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	res, err := fn(id)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrInvalidTransition):
			response.Conflict(c, "当前状态不允许该操作", nil)
		case errors.Is(err, model.ErrMachineBroken):
			response.Conflict(c, "机器故障中，不能开工", nil)
		case errors.Is(err, model.ErrEarlierStepUnfinished):
			response.Conflict(c, "该地块上前置作业步骤尚未完成，不能跳过顺序", nil)
		case errors.Is(err, sql.ErrNoRows):
			response.NotFound(c, "reservation not found")
		default:
			response.InternalError(c, err.Error())
		}
		return
	}
	response.Success(c, res)
}

// PlotProgress 这块地的作业做到哪一步了：GET /plots/:id/work-progress
func (h *ReservationHandler) PlotProgress(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	progress, err := h.svc.PlotProgress(id)
	if err != nil {
		response.NotFound(c, "plot not found")
		return
	}
	response.Success(c, progress)
}
