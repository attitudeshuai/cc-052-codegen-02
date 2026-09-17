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

type MachineHandler struct {
	svc *service.MachineService
}

func NewMachineHandler(svc *service.MachineService) *MachineHandler {
	return &MachineHandler{svc: svc}
}

func (h *MachineHandler) Create(c *gin.Context) {
	var req model.CreateMachineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	m, err := h.svc.Create(&req)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(c, "farm not found")
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Created(c, m)
}

func (h *MachineHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	m, err := h.svc.GetByID(id)
	if err != nil {
		response.NotFound(c, "machine not found")
		return
	}
	response.Success(c, m)
}

func (h *MachineHandler) List(c *gin.Context) {
	farmID, err := strconv.ParseInt(c.Query("farm_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid farm_id")
		return
	}
	machines, err := h.svc.ListByFarm(farmID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, machines)
}

// Breakdown 报坏：机器置为故障，未完成预约自动改派给同型号且空闲的机器
func (h *MachineHandler) Breakdown(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	result, err := h.svc.Breakdown(id)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrMachineBroken):
			response.Conflict(c, "machine already broken", nil)
		case errors.Is(err, sql.ErrNoRows):
			response.NotFound(c, "machine not found")
		default:
			response.InternalError(c, err.Error())
		}
		return
	}
	response.Success(c, result)
}

// Repair 修复：恢复为可用
func (h *MachineHandler) Repair(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	m, err := h.svc.Repair(id)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrMachineNotBroken):
			response.Conflict(c, "machine is not broken", nil)
		case errors.Is(err, sql.ErrNoRows):
			response.NotFound(c, "machine not found")
		default:
			response.InternalError(c, err.Error())
		}
		return
	}
	response.Success(c, m)
}

// Utilization 一台机器某天忙了多久、空着多久：GET /machines/:id/utilization?date=2026-09-17
func (h *MachineHandler) Utilization(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	u, err := h.svc.Utilization(id, c.Query("date"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(c, "machine not found")
			return
		}
		response.BadRequest(c, "invalid date, expect YYYY-MM-DD")
		return
	}
	response.Success(c, u)
}

// FarmUtilization 全场机器某天的忙闲汇总：GET /farms/:id/machine-utilization?date=2026-09-17
func (h *MachineHandler) FarmUtilization(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	list, err := h.svc.FarmUtilization(id, c.Query("date"))
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, list)
}
