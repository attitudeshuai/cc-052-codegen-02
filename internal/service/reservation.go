package service

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"fmt"
	"time"
)

// ConflictError 时段冲突：携带撞上的已有预约，handler 转成 409
type ConflictError struct {
	Conflicts []model.MachineReservation
}

func (e *ConflictError) Error() string {
	c := e.Conflicts[0]
	return fmt.Sprintf("时段冲突：与预约 #%d（%s ~ %s）重叠",
		c.ID, c.StartAt.Format(time.RFC3339), c.EndAt.Format(time.RFC3339))
}

type ReservationService struct {
	repo        *repository.ReservationRepo
	machineRepo *repository.MachineRepo
	plotRepo    *repository.PlotRepo
}

func NewReservationService(repo *repository.ReservationRepo, machineRepo *repository.MachineRepo, plotRepo *repository.PlotRepo) *ReservationService {
	return &ReservationService{repo: repo, machineRepo: machineRepo, plotRepo: plotRepo}
}

// Create 按时段预约：同一台机器时段重叠则拒绝并返回撞上的预约
func (s *ReservationService) Create(req *model.CreateReservationRequest) (*model.MachineReservation, error) {
	startAt, err := time.Parse(time.RFC3339, req.StartAt)
	if err != nil {
		return nil, fmt.Errorf("invalid start_at: %w", err)
	}
	endAt, err := time.Parse(time.RFC3339, req.EndAt)
	if err != nil {
		return nil, fmt.Errorf("invalid end_at: %w", err)
	}
	if !endAt.After(startAt) {
		return nil, fmt.Errorf("end_at must be after start_at")
	}

	// 机器与地块必须存在（repo 事务内还会再校验机器状态）
	if _, err := s.machineRepo.GetByID(req.MachineID); err != nil {
		return nil, err
	}
	if _, err := s.plotRepo.GetByID(req.PlotID); err != nil {
		return nil, err
	}

	res := &model.MachineReservation{
		MachineID: req.MachineID,
		PlotID:    req.PlotID,
		Operation: req.Operation,
		StartAt:   startAt,
		EndAt:     endAt,
	}
	conflicts, err := s.repo.CreateIfNoConflict(res)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, &ConflictError{Conflicts: conflicts}
	}
	return res, nil
}

func (s *ReservationService) GetByID(id int64) (*model.MachineReservation, error) {
	return s.repo.GetByID(id)
}

func (s *ReservationService) List(machineID, plotID *int64) ([]model.MachineReservation, error) {
	return s.repo.List(machineID, plotID)
}

// Start 开工：前置步骤必须全部完成，保证地块上作业先后顺序不乱
func (s *ReservationService) Start(id int64) (*model.MachineReservation, error) {
	res, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if res.Status != model.ReservationScheduled {
		return nil, model.ErrInvalidTransition
	}
	// 机器故障中不能开工
	machine, err := s.machineRepo.GetByID(res.MachineID)
	if err != nil {
		return nil, err
	}
	if machine.Status == model.MachineBroken {
		return nil, model.ErrMachineBroken
	}
	// 同一地块上 seq 更小的步骤必须先完成
	earlier, err := s.repo.HasEarlierUnfinished(res.PlotID, res.Seq)
	if err != nil {
		return nil, err
	}
	if earlier {
		return nil, model.ErrEarlierStepUnfinished
	}
	if err := s.repo.UpdateStatus(id, model.ReservationInProgress, model.ReservationScheduled); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id)
}

// Complete 完工
func (s *ReservationService) Complete(id int64) (*model.MachineReservation, error) {
	if err := s.repo.UpdateStatus(id, model.ReservationDone,
		model.ReservationScheduled, model.ReservationInProgress); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id)
}

// Cancel 取消（仅未开工的可取消）
func (s *ReservationService) Cancel(id int64) (*model.MachineReservation, error) {
	if err := s.repo.UpdateStatus(id, model.ReservationCancelled, model.ReservationScheduled); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id)
}

// PlotProgress 一块地的作业进度：做到哪一步了
func (s *ReservationService) PlotProgress(plotID int64) (*model.PlotWorkProgress, error) {
	plot, err := s.plotRepo.GetByID(plotID)
	if err != nil {
		return nil, err
	}
	steps, err := s.repo.ListActiveByPlot(plotID)
	if err != nil {
		return nil, err
	}
	progress := &model.PlotWorkProgress{
		PlotID:     plot.ID,
		PlotName:   plot.Name,
		TotalSteps: len(steps),
		Steps:      steps,
	}
	for i := range steps {
		if steps[i].Status == model.ReservationDone {
			progress.DoneSteps++
		} else if progress.CurrentStep == nil {
			step := steps[i]
			progress.CurrentStep = &step
		}
	}
	return progress, nil
}
