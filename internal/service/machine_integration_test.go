//go:build integration

package service

import (
	"cc-052/internal/model"
	"cc-052/internal/repository"
	"errors"
	"os"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var (
	testDB  *sqlx.DB
	testSvc *MachineService
)

func TestMain(m *testing.M) {
	dataDir, err := os.MkdirTemp("", "pgdata")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dataDir)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("farm").
		Password("farm_secret").
		Database("farm_trace").
		Port(15439).
		DataPath(dataDir).
		StartParameters(map[string]string{"fsync": "off", "full_page_writes": "off"}).
		StartTimeout(180 * time.Second).
		Version(embeddedpostgres.V16))
	if err := pg.Start(); err != nil {
		panic("start embedded postgres: " + err.Error())
	}
	db, err := sqlx.Connect("postgres",
		"host=localhost port=15439 user=farm password=farm_secret dbname=farm_trace sslmode=disable")
	if err != nil {
		panic("connect: " + err.Error())
	}
	if err := repository.RunMigrations(db, "../../migrations"); err != nil {
		panic("migrations: " + err.Error())
	}
	testDB = db
	testSvc = NewMachineService(
		repository.NewMachineRepo(db),
		repository.NewBookingRepo(db),
		repository.NewPlotRepo(db),
		repository.NewFarmRepo(db),
	)
	code := m.Run()
	testDB.Close()
	pg.Stop()
	os.Exit(code)
}

func mustExec(t *testing.T, query string) {
	t.Helper()
	if _, err := testDB.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func ptr(i int64) *int64 { return &i }

// 农机调度全流程：预约 → 撞单 → 地块顺序 → 报障 → 改派 → 进度 → 忙闲统计
func TestMachineryBookingFlow(t *testing.T) {
	mustExec(t, `INSERT INTO farm (name, region_code) VALUES ('合作社A', '370000')`)
	mustExec(t, `INSERT INTO plot (farm_id, name, area_mu, geojson, soil_type) VALUES (1, '东地', 12.5, '', '')`)
	mustExec(t, `INSERT INTO plot (farm_id, name, area_mu, geojson, soil_type) VALUES (1, '西地', 8.0, '', '')`)
	mustExec(t, `INSERT INTO machine (farm_id, name, model, kind) VALUES (1, '收割机-1', 'JM-2000', 'harvester')`)
	mustExec(t, `INSERT INTO machine (farm_id, name, model, kind) VALUES (1, '收割机-2', 'JM-2000', 'harvester')`)
	mustExec(t, `INSERT INTO machine (farm_id, name, model, kind) VALUES (1, '播种机-1', 'BZ-10', 'seeder')`)

	// 1. 正常预约：机器1 东地 犁地 8:00-10:00
	a, err := testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 1, PlotID: 1, WorkStep: "plow", StepSeq: 1,
		StartAt: "2026-09-17T08:00:00Z", EndAt: "2026-09-17T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("create booking A: %v", err)
	}

	// 2. 同一台机器时段重叠 → 拒绝，并指出撞上的是 A
	_, err = testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 1, PlotID: 2, WorkStep: "spray",
		StartAt: "2026-09-17T09:00:00Z", EndAt: "2026-09-17T11:00:00Z",
	})
	var ce *BookingConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want BookingConflictError, got %v", err)
	}
	if len(ce.Conflicts) != 1 || ce.Conflicts[0].ID != a.ID {
		t.Fatalf("conflict should point to booking A(%d), got %+v", a.ID, ce.Conflicts)
	}

	// 3. 首尾相接不算冲突：机器1 西地 10:00-11:00
	if _, err := testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 1, PlotID: 2, WorkStep: "spray",
		StartAt: "2026-09-17T10:00:00Z", EndAt: "2026-09-17T11:00:00Z",
	}); err != nil {
		t.Fatalf("adjacent booking should be accepted: %v", err)
	}

	// 4. 东地第二步：机器1 播种 11:00-12:00
	b, err := testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 1, PlotID: 1, WorkStep: "sow", StepSeq: 2,
		StartAt: "2026-09-17T11:00:00Z", EndAt: "2026-09-17T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("create booking B: %v", err)
	}

	// 5. 地块作业顺序：东地 seq3 排在 seq2(11-12点) 结束之前 → 拒绝
	_, err = testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 2, PlotID: 1, WorkStep: "spray", StepSeq: 3,
		StartAt: "2026-09-17T11:30:00Z", EndAt: "2026-09-17T12:30:00Z",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError for plot order, got %v", err)
	}

	// 6. 机器1 西地第二步 14:00-16:00
	c, err := testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 1, PlotID: 2, WorkStep: "harvest", StepSeq: 2,
		StartAt: "2026-09-17T14:00:00Z", EndAt: "2026-09-17T16:00:00Z",
	})
	if err != nil {
		t.Fatalf("create booking C: %v", err)
	}

	// 7. 机器1 报障 → 不再接受新预约
	if _, err := testSvc.UpdateMachineStatus(1, model.MachineBroken); err != nil {
		t.Fatalf("mark machine broken: %v", err)
	}
	_, err = testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 1, PlotID: 2, WorkStep: "plow",
		StartAt: "2026-09-17T18:00:00Z", EndAt: "2026-09-17T19:00:00Z",
	})
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError for broken machine, got %v", err)
	}

	// 8. 改派到型号不符的机器 → 拒绝
	_, err = testSvc.ReassignBooking(a.ID, &model.ReassignBookingRequest{MachineID: ptr(3)})
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError for model mismatch, got %v", err)
	}

	// 9. 一键改派故障机器的剩余预约 → 全部落到同型号的机器2，时段与地块顺序不变
	results, err := testSvc.ReassignRemaining(1)
	if err != nil {
		t.Fatalf("reassign remaining: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("want 4 reassigned bookings, got %+v", results)
	}
	for _, r := range results {
		if !r.OK || r.NewMachineID != 2 {
			t.Fatalf("booking %d should move to machine 2, got %+v", r.BookingID, r)
		}
	}
	// 型号不同的机器3 不能接到活
	other, err := testSvc.ListMachineBookings(3, "", "")
	if err != nil || len(other) != 0 {
		t.Fatalf("machine 3 should stay idle: err=%v bookings=%+v", err, other)
	}
	// 改派只换机器：原时段、step_seq 不动，地块上的先后顺序不乱
	a2, err := testSvc.GetBooking(a.ID)
	if err != nil {
		t.Fatalf("get booking A: %v", err)
	}
	if a2.MachineID != 2 || a2.StepSeq != 1 ||
		a2.StartAt.Format(time.RFC3339) != "2026-09-17T08:00:00Z" ||
		a2.EndAt.Format(time.RFC3339) != "2026-09-17T10:00:00Z" {
		t.Fatalf("reassign must keep original slot and step_seq: %+v", a2)
	}

	// 10. 地块进度：A 开工 → 完工，东地当前步应是 B
	if _, err := testSvc.UpdateBookingStatus(a.ID, model.BookingInProgress); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if _, err := testSvc.UpdateBookingStatus(a.ID, model.BookingDone); err != nil {
		t.Fatalf("finish A: %v", err)
	}
	prog, err := testSvc.PlotWorkProgress(1)
	if err != nil {
		t.Fatalf("plot progress: %v", err)
	}
	if prog.TotalSteps != 2 || prog.DoneSteps != 1 {
		t.Fatalf("want 1/2 done, got %+v", prog)
	}
	if prog.CurrentStep == nil || prog.CurrentStep.ID != b.ID || prog.CurrentStep.MachineName != "收割机-2" {
		t.Fatalf("current step should be B on 收割机-2, got %+v", prog.CurrentStep)
	}

	// 11. 机器2 当日忙闲：8-12点连续 4h + 14-16点 2h = 360 分钟
	stats, err := testSvc.MachineDailyStats(2, "2026-09-17")
	if err != nil {
		t.Fatalf("daily stats: %v", err)
	}
	if stats.BusyMinutes != 360 || stats.IdleMinutes != 1440-360 || stats.BookingCount != 4 {
		t.Fatalf("want busy=360 idle=1080 count=4, got %+v", stats)
	}

	// 12. 改派顺带改时段也不能打乱地块顺序：B(seq2) 挪到 9:00 会压在 A(seq1, 8-10) 上 → 拒绝
	_, err = testSvc.ReassignBooking(b.ID, &model.ReassignBookingRequest{
		MachineID: ptr(2),
		StartAt:   "2026-09-17T09:00:00Z",
		EndAt:     "2026-09-17T09:30:00Z",
	})
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError for plot order on moved slot, got %v", err)
	}

	// 13. 完工/取消的预约不再占时段：C 取消后，机器2 同时段可再约
	if _, err := testSvc.UpdateBookingStatus(c.ID, model.BookingCancelled); err != nil {
		t.Fatalf("cancel C: %v", err)
	}
	if _, err := testSvc.CreateBooking(&model.CreateBookingRequest{
		MachineID: 2, PlotID: 2, WorkStep: "plow", StepSeq: 2,
		StartAt: "2026-09-17T14:00:00Z", EndAt: "2026-09-17T16:00:00Z",
	}); err != nil {
		t.Fatalf("slot of cancelled booking should be reusable: %v", err)
	}
}
