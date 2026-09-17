package service

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrMachineNotFound = errors.New("machine not found")
	ErrBookingNotFound = errors.New("booking not found")
	ErrPlotNotFound    = errors.New("plot not found")
	ErrFarmNotFound    = errors.New("farm not found")
)

// ValidationError 请求本身不合法（时段倒置、型号不符、状态机不允许等）→ 400
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// BookingConflictError 时段与已有预约重叠 → 409，携带撞上的预约明细
type BookingConflictError struct {
	Conflicts []model.Booking
}

func (e *BookingConflictError) Error() string {
	return fmt.Sprintf("time slot overlaps with %d existing booking(s) on this machine", len(e.Conflicts))
}

type MachineService struct {
	machineRepo *repository.MachineRepo
	bookingRepo *repository.BookingRepo
	plotRepo    *repository.PlotRepo
	farmRepo    *repository.FarmRepo
}

func NewMachineService(machineRepo *repository.MachineRepo, bookingRepo *repository.BookingRepo,
	plotRepo *repository.PlotRepo, farmRepo *repository.FarmRepo) *MachineService {
	return &MachineService{machineRepo: machineRepo, bookingRepo: bookingRepo, plotRepo: plotRepo, farmRepo: farmRepo}
}

// ---------- 农机档案 ----------

func (s *MachineService) CreateMachine(req *model.CreateMachineRequest) (*model.Machine, error) {
	if _, err := s.farmRepo.GetByID(req.FarmID); err != nil {
		return nil, ErrFarmNotFound
	}
	kind := req.Kind
	if kind == "" {
		kind = "tractor"
	}
	m := &model.Machine{
		FarmID: req.FarmID,
		Name:   req.Name,
		Model:  req.Model,
		Kind:   kind,
		Status: model.MachineAvailable,
	}
	if err := s.machineRepo.Create(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *MachineService) GetMachine(id int64) (*model.Machine, error) {
	m, err := s.machineRepo.GetByID(id)
	if err != nil {
		return nil, ErrMachineNotFound
	}
	return m, nil
}

func (s *MachineService) ListMachines(farmID int64) ([]model.Machine, error) {
	return s.machineRepo.ListByFarm(farmID)
}

// UpdateMachineStatus 报障/维修/恢复。置 broken 不会取消已有预约，需另行改派。
func (s *MachineService) UpdateMachineStatus(id int64, status model.MachineStatus) (*model.Machine, error) {
	switch status {
	case model.MachineAvailable, model.MachineMaintenance, model.MachineBroken:
	default:
		return nil, &ValidationError{Msg: "status must be one of available|maintenance|broken"}
	}
	if _, err := s.machineRepo.GetByID(id); err != nil {
		return nil, ErrMachineNotFound
	}
	if err := s.machineRepo.UpdateStatus(id, status); err != nil {
		return nil, err
	}
	return s.machineRepo.GetByID(id)
}

// ---------- 按时段预约 ----------

func (s *MachineService) CreateBooking(req *model.CreateBookingRequest) (*model.Booking, error) {
	machine, err := s.machineRepo.GetByID(req.MachineID)
	if err != nil {
		return nil, ErrMachineNotFound
	}
	plot, err := s.plotRepo.GetByID(req.PlotID)
	if err != nil {
		return nil, ErrPlotNotFound
	}
	if machine.FarmID != plot.FarmID {
		return nil, &ValidationError{Msg: "machine and plot belong to different farms"}
	}
	start, err := parseTime(req.StartAt)
	if err != nil {
		return nil, &ValidationError{Msg: "invalid start_at: " + err.Error()}
	}
	end, err := parseTime(req.EndAt)
	if err != nil {
		return nil, &ValidationError{Msg: "invalid end_at: " + err.Error()}
	}
	if !end.After(start) {
		return nil, &ValidationError{Msg: "end_at must be after start_at"}
	}

	seq := req.StepSeq
	if seq <= 0 {
		maxSeq, err := s.bookingRepo.MaxStepSeq(req.PlotID)
		if err != nil {
			return nil, err
		}
		seq = maxSeq + 1
	}

	// 同一地块上：序号在前的作业必须先结束，序号在后的必须更晚开始
	steps, err := s.bookingRepo.ListByPlot(req.PlotID)
	if err != nil {
		return nil, err
	}
	if err := checkPlotOrder(steps, 0, seq, start, end); err != nil {
		return nil, err
	}

	b := &model.Booking{
		MachineID: req.MachineID,
		PlotID:    req.PlotID,
		WorkStep:  req.WorkStep,
		StepSeq:   seq,
		StartAt:   start,
		EndAt:     end,
		Status:    model.BookingScheduled,
		Note:      req.Note,
	}
	conflicts, err := s.bookingRepo.CreateTx(b)
	if err != nil {
		return nil, mapRepoError(err)
	}
	if len(conflicts) > 0 {
		return nil, &BookingConflictError{Conflicts: conflicts}
	}
	return b, nil
}

func (s *MachineService) GetBooking(id int64) (*model.Booking, error) {
	b, err := s.bookingRepo.GetByID(id)
	if err != nil {
		return nil, ErrBookingNotFound
	}
	return b, nil
}

// UpdateBookingStatus 推进预约状态：scheduled→in_progress→done，前两者可 cancelled
func (s *MachineService) UpdateBookingStatus(id int64, next model.BookingStatus) (*model.Booking, error) {
	b, err := s.bookingRepo.GetByID(id)
	if err != nil {
		return nil, ErrBookingNotFound
	}
	allowed := map[model.BookingStatus][]model.BookingStatus{
		model.BookingScheduled:  {model.BookingInProgress, model.BookingCancelled},
		model.BookingInProgress: {model.BookingDone, model.BookingCancelled},
	}
	ok := false
	for _, st := range allowed[b.Status] {
		if st == next {
			ok = true
			break
		}
	}
	if !ok {
		return nil, &ValidationError{Msg: fmt.Sprintf("cannot move booking from %s to %s", b.Status, next)}
	}
	// 坏机器不能开工，先改派
	if next == model.BookingInProgress {
		m, err := s.machineRepo.GetByID(b.MachineID)
		if err != nil {
			return nil, ErrMachineNotFound
		}
		if m.Status != model.MachineAvailable {
			return nil, &ValidationError{Msg: "machine is not available; reassign the booking first"}
		}
	}
	if err := s.bookingRepo.UpdateStatus(id, next); err != nil {
		return nil, err
	}
	return s.bookingRepo.GetByID(id)
}

// ---------- 故障改派 ----------

// ReassignBooking 把预约改派给另一台同型号机器；MachineID 为空时自动挑一台同型号且该时段空闲的。
// 只换机器、默认保留原时段与 step_seq，因此地块上原有作业顺序不变；
// 若显式给了新时段，则校验不打乱该地块的作业先后顺序。
func (s *MachineService) ReassignBooking(bookingID int64, req *model.ReassignBookingRequest) (*model.Booking, error) {
	b, err := s.bookingRepo.GetByID(bookingID)
	if err != nil {
		return nil, ErrBookingNotFound
	}
	if b.Status != model.BookingScheduled && b.Status != model.BookingInProgress {
		return nil, &ValidationError{Msg: fmt.Sprintf("booking is %s, only scheduled/in_progress can be reassigned", b.Status)}
	}
	src, err := s.machineRepo.GetByID(b.MachineID)
	if err != nil {
		return nil, ErrMachineNotFound
	}

	start, end := b.StartAt, b.EndAt
	slotChanged := false
	if req.StartAt != "" || req.EndAt != "" {
		if req.StartAt == "" || req.EndAt == "" {
			return nil, &ValidationError{Msg: "start_at and end_at must be provided together"}
		}
		start, err = parseTime(req.StartAt)
		if err != nil {
			return nil, &ValidationError{Msg: "invalid start_at: " + err.Error()}
		}
		end, err = parseTime(req.EndAt)
		if err != nil {
			return nil, &ValidationError{Msg: "invalid end_at: " + err.Error()}
		}
		if !end.After(start) {
			return nil, &ValidationError{Msg: "end_at must be after start_at"}
		}
		slotChanged = true
	}
	if slotChanged {
		steps, err := s.bookingRepo.ListByPlot(b.PlotID)
		if err != nil {
			return nil, err
		}
		if err := checkPlotOrder(steps, b.ID, b.StepSeq, start, end); err != nil {
			return nil, err
		}
	}

	if req.MachineID != nil {
		target, err := s.machineRepo.GetByID(*req.MachineID)
		if err != nil {
			return nil, ErrMachineNotFound
		}
		if err := checkReassignTarget(src, target); err != nil {
			return nil, err
		}
		conflicts, err := s.bookingRepo.ReassignTx(bookingID, target.ID, start, end)
		if err != nil {
			return nil, mapRepoError(err)
		}
		if len(conflicts) > 0 {
			return nil, &BookingConflictError{Conflicts: conflicts}
		}
		return s.bookingRepo.GetByID(bookingID)
	}

	// 自动挑选：同农场同型号且可用，逐台试，第一台该时段空闲的胜出
	candidates, err := s.machineRepo.ListAvailableSameModel(src.FarmID, src.Model, src.ID)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, &ValidationError{Msg: fmt.Sprintf("no other available machine of model %q", src.Model)}
	}
	var lastConflicts []model.Booking
	for _, cand := range candidates {
		conflicts, err := s.bookingRepo.ReassignTx(bookingID, cand.ID, start, end)
		if err != nil {
			if errors.Is(err, repository.ErrMachineUnavailable) {
				continue // 刚被报障，试下一台
			}
			return nil, mapRepoError(err)
		}
		if len(conflicts) == 0 {
			return s.bookingRepo.GetByID(bookingID)
		}
		lastConflicts = conflicts
	}
	if lastConflicts != nil {
		return nil, &BookingConflictError{Conflicts: lastConflicts}
	}
	return nil, &ValidationError{Msg: "no same-model machine is free in this time slot"}
}

// ReassignRemaining 机器坏了之后，把它剩余（scheduled/in_progress）的预约逐条自动改派出去
func (s *MachineService) ReassignRemaining(machineID int64) ([]model.ReassignResult, error) {
	if _, err := s.machineRepo.GetByID(machineID); err != nil {
		return nil, ErrMachineNotFound
	}
	actives, err := s.bookingRepo.ListActiveByMachine(machineID)
	if err != nil {
		return nil, err
	}
	results := make([]model.ReassignResult, 0, len(actives))
	for _, b := range actives {
		nb, err := s.ReassignBooking(b.ID, &model.ReassignBookingRequest{})
		if err != nil {
			results = append(results, model.ReassignResult{BookingID: b.ID, Error: err.Error()})
			continue
		}
		results = append(results, model.ReassignResult{BookingID: b.ID, OK: true, NewMachineID: nb.MachineID})
	}
	return results, nil
}

// ---------- 进度与忙闲 ----------

// PlotWorkProgress 这块地的作业做到哪一步
func (s *MachineService) PlotWorkProgress(plotID int64) (*model.PlotWorkProgress, error) {
	plot, err := s.plotRepo.GetByID(plotID)
	if err != nil {
		return nil, ErrPlotNotFound
	}
	steps, err := s.bookingRepo.ListByPlot(plotID)
	if err != nil {
		return nil, err
	}
	progress := &model.PlotWorkProgress{
		PlotID:     plot.ID,
		PlotName:   plot.Name,
		TotalSteps: len(steps),
		Steps:      steps,
	}
	for i := range progress.Steps {
		if progress.Steps[i].Status == model.BookingDone {
			progress.DoneSteps++
		}
	}
	// 当前步：优先正在进行中的，否则取最近一条待执行的
	for i := range progress.Steps {
		if progress.Steps[i].Status == model.BookingInProgress {
			progress.CurrentStep = &progress.Steps[i]
			break
		}
	}
	if progress.CurrentStep == nil {
		for i := range progress.Steps {
			if progress.Steps[i].Status == model.BookingScheduled {
				progress.CurrentStep = &progress.Steps[i]
				break
			}
		}
	}
	return progress, nil
}

// MachineDailyStats 一台机器某一天（UTC 自然日）忙了多久、空着多久
func (s *MachineService) MachineDailyStats(machineID int64, dateStr string) (*model.MachineDailyStats, error) {
	m, err := s.machineRepo.GetByID(machineID)
	if err != nil {
		return nil, ErrMachineNotFound
	}
	day, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, &ValidationError{Msg: "invalid date, want YYYY-MM-DD"}
	}
	from := day.UTC()
	to := from.Add(24 * time.Hour)
	bookings, err := s.bookingRepo.ListByMachineRange(machineID, from, to)
	if err != nil {
		return nil, err
	}
	busy := mergeBusyMinutes(bookings, from, to)
	return &model.MachineDailyStats{
		MachineID:    m.ID,
		MachineName:  m.Name,
		Date:         dateStr,
		BusyMinutes:  busy,
		IdleMinutes:  int(to.Sub(from).Minutes()) - busy,
		BookingCount: len(bookings),
		Bookings:     bookings,
	}, nil
}

// ListMachineBookings 机器排班表；from/to 为空表示不限
func (s *MachineService) ListMachineBookings(machineID int64, fromStr, toStr string) ([]model.Booking, error) {
	if _, err := s.machineRepo.GetByID(machineID); err != nil {
		return nil, ErrMachineNotFound
	}
	from := time.Time{}
	to := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	var err error
	if fromStr != "" {
		if from, err = parseTime(fromStr); err != nil {
			return nil, &ValidationError{Msg: "invalid from: " + err.Error()}
		}
	}
	if toStr != "" {
		if to, err = parseTime(toStr); err != nil {
			return nil, &ValidationError{Msg: "invalid to: " + err.Error()}
		}
	}
	return s.bookingRepo.ListByMachineRange(machineID, from, to)
}

// ---------- 内部工具 ----------

func checkReassignTarget(src, target *model.Machine) error {
	if target.FarmID != src.FarmID {
		return &ValidationError{Msg: "target machine belongs to a different farm"}
	}
	if target.Model != src.Model {
		return &ValidationError{Msg: fmt.Sprintf("target machine model %q does not match required model %q", target.Model, src.Model)}
	}
	return nil
}

// checkPlotOrder 校验地块作业顺序：序号更小的作业必须先结束，序号更大的必须更晚开始（序号相同允许并行）
func checkPlotOrder(steps []model.PlotStepView, selfID int64, selfSeq int, start, end time.Time) error {
	for _, st := range steps {
		if st.ID == selfID {
			continue
		}
		if st.StepSeq < selfSeq && st.EndAt.After(start) {
			return &ValidationError{Msg: fmt.Sprintf(
				"plot step order violated: earlier step #%d (%s) ends at %s, this step must start after it",
				st.StepSeq, st.WorkStep, st.EndAt.Format(time.RFC3339))}
		}
		if st.StepSeq > selfSeq && st.StartAt.Before(end) {
			return &ValidationError{Msg: fmt.Sprintf(
				"plot step order violated: later step #%d (%s) starts at %s, this step must finish before it",
				st.StepSeq, st.WorkStep, st.StartAt.Format(time.RFC3339))}
		}
	}
	return nil
}

// mergeBusyMinutes 把预约区间裁剪到 [from, to) 后合并，算总占用分钟数
func mergeBusyMinutes(bookings []model.Booking, from, to time.Time) int {
	type interval struct{ s, e time.Time }
	ivs := make([]interval, 0, len(bookings))
	for _, b := range bookings {
		s := b.StartAt
		if s.Before(from) {
			s = from
		}
		e := b.EndAt
		if e.After(to) {
			e = to
		}
		if e.After(s) {
			ivs = append(ivs, interval{s, e})
		}
	}
	if len(ivs) == 0 {
		return 0
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].s.Before(ivs[j].s) })
	total := 0
	cs, ce := ivs[0].s, ivs[0].e
	for _, iv := range ivs[1:] {
		if iv.s.After(ce) {
			total += int(ce.Sub(cs).Minutes())
			cs, ce = iv.s, iv.e
		} else if iv.e.After(ce) {
			ce = iv.e
		}
	}
	total += int(ce.Sub(cs).Minutes())
	return total
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
	}
	return t, err
}

func mapRepoError(err error) error {
	switch {
	case errors.Is(err, repository.ErrMachineUnavailable):
		return &ValidationError{Msg: "machine is not available (broken or under maintenance)"}
	case errors.Is(err, repository.ErrInvalidBookingState):
		return &ValidationError{Msg: "booking state does not allow this operation"}
	case errors.Is(err, sql.ErrNoRows):
		return ErrBookingNotFound
	default:
		return err
	}
}
