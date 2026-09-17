package service

import (
	"cc-052/internal/model"
	"testing"
	"time"
)

func at(day, hour int) time.Time {
	return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC)
}

func resv(id, machineID int64, startDay, startHour, endDay, endHour int) model.MachineReservation {
	return model.MachineReservation{
		ID:        id,
		MachineID: machineID,
		StartAt:   at(startDay, startHour),
		EndAt:     at(endDay, endHour),
	}
}

func TestSlotsOverlap(t *testing.T) {
	cases := []struct {
		name                 string
		aStart, aEnd, bStart time.Time
		bEnd                 time.Time
		want                 bool
	}{
		{"完全重叠", at(1, 8), at(1, 12), at(1, 8), at(1, 12), true},
		{"部分重叠-前", at(1, 8), at(1, 12), at(1, 10), at(1, 14), true},
		{"部分重叠-后", at(1, 10), at(1, 14), at(1, 8), at(1, 12), true},
		{"包含", at(1, 8), at(1, 18), at(1, 10), at(1, 12), true},
		{"被包含", at(1, 10), at(1, 12), at(1, 8), at(1, 18), true},
		{"首尾相接不算冲突", at(1, 8), at(1, 12), at(1, 12), at(1, 16), false},
		{"首尾相接-反向", at(1, 12), at(1, 16), at(1, 8), at(1, 12), false},
		{"完全不相交", at(1, 8), at(1, 10), at(1, 12), at(1, 14), false},
		{"跨天重叠", at(1, 20), at(2, 6), at(2, 4), at(2, 10), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := slotsOverlap(c.aStart, c.aEnd, c.bStart, c.bEnd); got != c.want {
				t.Errorf("slotsOverlap(%v,%v,%v,%v) = %v, want %v",
					c.aStart, c.aEnd, c.bStart, c.bEnd, got, c.want)
			}
		})
	}
}

func TestPlanReassignment_SingleMachineTakesAll(t *testing.T) {
	pending := []model.MachineReservation{
		resv(1, 1, 2, 8, 2, 10),
		resv(2, 1, 2, 13, 2, 15),
	}
	candidates := []model.Machine{{ID: 2}, {ID: 3}}
	busy := map[int64][]model.MachineReservation{
		2: {resv(9, 2, 2, 10, 2, 12)}, // 机器2中间有占用，但恰好不撞
		3: {},
	}
	got := PlanReassignment(pending, candidates, busy)
	// 机器2能吞下全部 -> 整体改派给2
	if len(got) != 2 || got[1] != 2 || got[2] != 2 {
		t.Fatalf("expected all -> machine 2, got %v", got)
	}
}

func TestPlanReassignment_SplitWhenNoSingleFits(t *testing.T) {
	pending := []model.MachineReservation{
		resv(1, 1, 2, 8, 2, 10),
		resv(2, 1, 2, 13, 2, 15),
	}
	candidates := []model.Machine{{ID: 2}, {ID: 3}}
	busy := map[int64][]model.MachineReservation{
		2: {resv(9, 2, 2, 8, 2, 10)},  // 机器2 早上被占
		3: {resv(8, 3, 2, 13, 2, 15)}, // 机器3 下午被占
	}
	got := PlanReassignment(pending, candidates, busy)
	// 没有一台能全吞 -> 拆分：#1->3, #2->2
	if got[1] != 3 || got[2] != 2 {
		t.Fatalf("expected split #1->3 #2->2, got %v", got)
	}
}

func TestPlanReassignment_UnassignedWhenAllBusy(t *testing.T) {
	pending := []model.MachineReservation{
		resv(1, 1, 2, 8, 2, 10),
	}
	candidates := []model.Machine{{ID: 2}}
	busy := map[int64][]model.MachineReservation{
		2: {resv(9, 2, 2, 8, 2, 10)},
	}
	got := PlanReassignment(pending, candidates, busy)
	if len(got) != 0 {
		t.Fatalf("expected no assignment, got %v", got)
	}
}

func TestPlanReassignment_NoCandidates(t *testing.T) {
	pending := []model.MachineReservation{resv(1, 1, 2, 8, 2, 10)}
	if got := PlanReassignment(pending, nil, nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestPlanReassignment_BackToBackSlots(t *testing.T) {
	// 同一台坏机器上的预约首尾相接（合法），改派时逐条找空闲机器
	pending := []model.MachineReservation{
		resv(1, 1, 2, 8, 2, 10),
		resv(2, 1, 2, 10, 2, 12),
	}
	candidates := []model.Machine{{ID: 2}, {ID: 3}}
	busy := map[int64][]model.MachineReservation{
		2: {resv(9, 2, 2, 10, 2, 12)}, // 机器2 下午段被占
		3: {resv(8, 3, 2, 8, 2, 10)},  // 机器3 上午段被占
	}
	got := PlanReassignment(pending, candidates, busy)
	// 没有一台能全吞 -> #1->2, #2->3
	if got[1] != 2 || got[2] != 3 {
		t.Fatalf("expected #1->2 #2->3, got %v", got)
	}
}

func TestOverlapMinutes(t *testing.T) {
	dayStart := at(2, 0)
	dayEnd := at(3, 0)
	cases := []struct {
		name       string
		start, end time.Time
		want       int
	}{
		{"全天内", at(2, 8), at(2, 10), 120},
		{"跨左边界", at(1, 22), at(2, 2), 120},
		{"跨右边界", at(2, 23), at(3, 1), 60},
		{"覆盖全天", at(1, 0), at(4, 0), 1440},
		{"窗口外-前", at(1, 8), at(1, 10), 0},
		{"窗口外-后", at(3, 8), at(3, 10), 0},
		{"贴着左边界", at(1, 22), at(2, 0), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := overlapMinutes(c.start, c.end, dayStart, dayEnd); got != c.want {
				t.Errorf("overlapMinutes = %d, want %d", got, c.want)
			}
		})
	}
}
