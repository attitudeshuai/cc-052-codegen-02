package service

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"time"
)

type MachineService struct {
	repo     *repository.MachineRepo
	resvRepo *repository.ReservationRepo
	farmRepo *repository.FarmRepo
}

func NewMachineService(repo *repository.MachineRepo, resvRepo *repository.ReservationRepo, farmRepo *repository.FarmRepo) *MachineService {
	return &MachineService{repo: repo, resvRepo: resvRepo, farmRepo: farmRepo}
}

func (s *MachineService) Create(req *model.CreateMachineRequest) (*model.Machine, error) {
	// 农场必须存在
	if _, err := s.farmRepo.GetByID(req.FarmID); err != nil {
		return nil, err
	}
	m := &model.Machine{
		FarmID: req.FarmID,
		Name:   req.Name,
		Model:  req.Model,
	}
	if err := s.repo.Create(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *MachineService) GetByID(id int64) (*model.Machine, error) {
	return s.repo.GetByID(id)
}

func (s *MachineService) ListByFarm(farmID int64) ([]model.Machine, error) {
	return s.repo.ListByFarm(farmID)
}

// Breakdown 报坏：机器置为 broken，未完成预约改派给同型号且空闲的机器
func (s *MachineService) Breakdown(id int64) (*model.BreakdownResult, error) {
	machine, reassigned, unassigned, err := s.repo.BreakdownAndReassign(id, PlanReassignment)
	if err != nil {
		return nil, err
	}
	return &model.BreakdownResult{
		Machine:    machine,
		Reassigned: reassigned,
		Unassigned: unassigned,
	}, nil
}

// Repair 修复：broken -> available
func (s *MachineService) Repair(id int64) (*model.Machine, error) {
	return s.repo.Repair(id)
}

const dayMinutes = 24 * 60

// Utilization 一台机器某一天（UTC）忙了多久、空着多久
func (s *MachineService) Utilization(machineID int64, date string) (*model.MachineUtilization, error) {
	m, err := s.repo.GetByID(machineID)
	if err != nil {
		return nil, err
	}
	dayStart, err := parseDateOrToday(date)
	if err != nil {
		return nil, err
	}
	dayEnd := dayStart.Add(24 * time.Hour)

	reservations, err := s.resvRepo.ListByMachineAndRange(machineID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}

	u := &model.MachineUtilization{
		MachineID:   m.ID,
		MachineName: m.Name,
		Model:       m.Model,
		Status:      m.Status,
		Date:        dayStart.Format("2006-01-02"),
		Slots:       []model.UtilizationSlot{},
	}
	for _, r := range reservations {
		minutes := overlapMinutes(r.StartAt, r.EndAt, dayStart, dayEnd)
		if minutes <= 0 {
			continue
		}
		u.BusyMinutes += minutes
		u.Slots = append(u.Slots, model.UtilizationSlot{
			ReservationID: r.ID,
			PlotID:        r.PlotID,
			Operation:     r.Operation,
			StartAt:       r.StartAt,
			EndAt:         r.EndAt,
			Status:        r.Status,
			Minutes:       minutes,
		})
	}
	if u.BusyMinutes > dayMinutes {
		u.BusyMinutes = dayMinutes
	}
	u.IdleMinutes = dayMinutes - u.BusyMinutes
	return u, nil
}

// FarmUtilization 一个农场全部机器某天的忙闲汇总
func (s *MachineService) FarmUtilization(farmID int64, date string) ([]model.MachineUtilization, error) {
	machines, err := s.repo.ListByFarm(farmID)
	if err != nil {
		return nil, err
	}
	result := make([]model.MachineUtilization, 0, len(machines))
	for _, m := range machines {
		u, err := s.Utilization(m.ID, date)
		if err != nil {
			return nil, err
		}
		result = append(result, *u)
	}
	return result, nil
}

func parseDateOrToday(date string) (time.Time, error) {
	if date == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, err
	}
	return day, nil
}
