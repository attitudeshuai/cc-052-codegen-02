BEGIN;

-- Farm machinery (harvesters, tractors, seeders shared across plots)
CREATE TABLE IF NOT EXISTS machine (
    id BIGSERIAL PRIMARY KEY,
    farm_id BIGINT NOT NULL REFERENCES farm(id),
    name VARCHAR(255) NOT NULL,
    model VARCHAR(128) NOT NULL, -- 型号：故障改派时只允许换同型号的机器
    kind VARCHAR(32) NOT NULL DEFAULT 'tractor',
    status VARCHAR(16) NOT NULL DEFAULT 'available' CHECK (status IN ('available','maintenance','broken')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Time-slot bookings: one machine works one plot for [start_at, end_at)
CREATE TABLE IF NOT EXISTS booking (
    id BIGSERIAL PRIMARY KEY,
    machine_id BIGINT NOT NULL REFERENCES machine(id),
    plot_id BIGINT NOT NULL REFERENCES plot(id),
    work_step VARCHAR(64) NOT NULL, -- 作业环节：plow/sow/spray/harvest...
    step_seq INT NOT NULL DEFAULT 1, -- 同一地块上作业的先后顺序
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','in_progress','done','cancelled')),
    note VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_at > start_at)
);

-- 同一台机器按时段查冲突 / 拉排班
CREATE INDEX IF NOT EXISTS idx_booking_machine_time ON booking(machine_id, start_at, end_at) WHERE status <> 'cancelled';
-- 地块作业进度按 step_seq 排序取
CREATE INDEX IF NOT EXISTS idx_booking_plot ON booking(plot_id, step_seq, start_at);
CREATE INDEX IF NOT EXISTS idx_machine_farm ON machine(farm_id);

COMMIT;
