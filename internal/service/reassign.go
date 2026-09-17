package service

import (
	"cc-052/internal/model"
	"time"
)

// slotsOverlap 两个时段是否重叠（首尾相接不算重叠，方便一台机器连轴转）
func slotsOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}

func slotFree(busy []model.MachineReservation, start, end time.Time) bool {
	for _, b := range busy {
		if slotsOverlap(b.StartAt, b.EndAt, start, end) {
			return false
		}
	}
	return true
}

// PlanReassignment 故障改派的纯函数规划器：
// pending 按开始时间升序（来自同一台机器，按构造互不重叠）；candidates 为同型号空闲机器（按 id 升序，保证结果确定）。
// 优先找一台能吞下全部预约的机器（车队整齐）；找不到再逐条塞给第一台空闲机器。
// 只改机器，不动时段与 seq —— 地块上原有的作业先后顺序保持不变。
func PlanReassignment(pending []model.MachineReservation, candidates []model.Machine, busy map[int64][]model.MachineReservation) map[int64]int64 {
	if len(pending) == 0 || len(candidates) == 0 {
		return nil
	}

	// 1) 优先整体改派给同一台机器
	for _, c := range candidates {
		allFree := true
		for _, p := range pending {
			if !slotFree(busy[c.ID], p.StartAt, p.EndAt) {
				allFree = false
				break
			}
		}
		if allFree {
			assignments := make(map[int64]int64, len(pending))
			for _, p := range pending {
				assignments[p.ID] = c.ID
			}
			return assignments
		}
	}

	// 2) 逐条改派：每条预约找第一台该时段空闲的机器；已派出的时段计入占用
	workBusy := make(map[int64][]model.MachineReservation, len(busy))
	for id, slots := range busy {
		workBusy[id] = append([]model.MachineReservation(nil), slots...)
	}
	assignments := make(map[int64]int64, len(pending))
	for _, p := range pending {
		for _, c := range candidates {
			if slotFree(workBusy[c.ID], p.StartAt, p.EndAt) {
				assignments[p.ID] = c.ID
				workBusy[c.ID] = append(workBusy[c.ID], p)
				break
			}
		}
	}
	if len(assignments) == 0 {
		return nil
	}
	return assignments
}

// overlapMinutes 时段 [start,end) 落在窗口 [winStart,winEnd) 内的分钟数
func overlapMinutes(start, end, winStart, winEnd time.Time) int {
	if end.Before(winStart) || end.Equal(winStart) || start.After(winEnd) || start.Equal(winEnd) {
		return 0
	}
	if start.Before(winStart) {
		start = winStart
	}
	if end.After(winEnd) {
		end = winEnd
	}
	return int(end.Sub(start).Minutes())
}
