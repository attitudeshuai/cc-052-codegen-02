package service

import (
	"cc-052/internal/model"
	"testing"
	"time"
)

func at(day, hour, min int) time.Time {
	return time.Date(2026, 9, day, hour, min, 0, 0, time.UTC)
}

func bookingBetween(id int64, s, e time.Time) model.Booking {
	return model.Booking{ID: id, StartAt: s, EndAt: e, Status: model.BookingScheduled}
}

func TestMergeBusyMinutes(t *testing.T) {
	from := at(17, 0, 0)
	to := at(18, 0, 0)

	tests := []struct {
		name     string
		bookings []model.Booking
		want     int
	}{
		{"empty", nil, 0},
		{"single inside window", []model.Booking{bookingBetween(1, at(17, 8, 0), at(17, 10, 0))}, 120},
		{"clipped at both ends", []model.Booking{bookingBetween(1, at(16, 23, 0), at(18, 1, 0))}, 1440},
		{"overlap merged once", []model.Booking{
			bookingBetween(1, at(17, 8, 0), at(17, 12, 0)),
			bookingBetween(2, at(17, 10, 0), at(17, 14, 0)),
		}, 360}, // 8:00-14:00 并集
		{"disjoint summed", []model.Booking{
			bookingBetween(1, at(17, 8, 0), at(17, 9, 0)),
			bookingBetween(2, at(17, 17, 0), at(17, 19, 0)),
		}, 180},
		{"touching counts both", []model.Booking{
			bookingBetween(1, at(17, 8, 0), at(17, 10, 0)),
			bookingBetween(2, at(17, 10, 0), at(17, 12, 0)),
		}, 240},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeBusyMinutes(tt.bookings, from, to); got != tt.want {
				t.Errorf("mergeBusyMinutes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func step(id int64, seq int, s, e time.Time) model.PlotStepView {
	return model.PlotStepView{Booking: model.Booking{
		ID: id, StepSeq: seq, WorkStep: "plow", StartAt: s, EndAt: e, Status: model.BookingScheduled,
	}}
}

func TestCheckPlotOrder(t *testing.T) {
	steps := []model.PlotStepView{
		step(1, 1, at(17, 8, 0), at(17, 10, 0)),  // 前序作业 8-10 点
		step(2, 3, at(17, 14, 0), at(17, 16, 0)), // 后序作业 14-16 点
	}

	tests := []struct {
		name    string
		selfID  int64
		selfSeq int
		start   time.Time
		end     time.Time
		wantErr bool
	}{
		{"fits between steps", 0, 2, at(17, 10, 0), at(17, 14, 0), false},
		{"starts before earlier step ends", 0, 2, at(17, 9, 30), at(17, 11, 0), true},
		{"ends after later step starts", 0, 2, at(17, 12, 0), at(17, 14, 30), true},
		{"touching boundaries ok", 0, 2, at(17, 10, 0), at(17, 14, 0), false},
		{"same seq may run parallel", 0, 1, at(17, 8, 30), at(17, 9, 30), false},
		{"self is skipped", 1, 1, at(17, 8, 0), at(17, 10, 0), false},
		{"appended after last step", 0, 4, at(17, 16, 0), at(17, 18, 0), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPlotOrder(steps, tt.selfID, tt.selfSeq, tt.start, tt.end)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkPlotOrder() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
