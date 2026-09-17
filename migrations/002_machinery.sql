BEGIN;

-- Farm machines (农机档案)
CREATE TABLE IF NOT EXISTS machine (
    id BIGSERIAL PRIMARY KEY,
    farm_id BIGINT NOT NULL REFERENCES farm(id),
    name VARCHAR(255) NOT NULL,
    model VARCHAR(64) NOT NULL,          -- 型号：故障改派时按同型号匹配
    status VARCHAR(16) NOT NULL DEFAULT 'available' CHECK (status IN ('available','broken')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Time-slot reservations (按时段预约)
CREATE TABLE IF NOT EXISTS machine_reservation (
    id BIGSERIAL PRIMARY KEY,
    machine_id BIGINT NOT NULL REFERENCES machine(id),
    plot_id BIGINT NOT NULL REFERENCES plot(id),
    operation VARCHAR(32) NOT NULL,      -- 作业类型：plow/sow/spray/harvest 等
    seq INT NOT NULL,                    -- 同一地块上的作业先后顺序（自动递增）
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','in_progress','done','cancelled')),
    reassigned_from BIGINT,              -- 若由故障改派而来，记录原机器 id
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_at > start_at)
);

-- 同一台机器查重叠时段
CREATE INDEX IF NOT EXISTS idx_reservation_machine_time
    ON machine_reservation (machine_id, start_at, end_at) WHERE status <> 'cancelled';

-- 同一地块上的作业顺序：未取消的预约中 seq 唯一，保证先后顺序不乱
CREATE UNIQUE INDEX IF NOT EXISTS uniq_reservation_plot_active_seq
    ON machine_reservation (plot_id, seq) WHERE status <> 'cancelled';

CREATE INDEX IF NOT EXISTS idx_reservation_plot
    ON machine_reservation (plot_id) WHERE status <> 'cancelled';

CREATE INDEX IF NOT EXISTS idx_machine_farm ON machine (farm_id);

COMMIT;
