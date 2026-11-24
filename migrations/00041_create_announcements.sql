-- +goose Up
-- Staff announcements: one row each, whoever they reach. The audience is
-- every organization ('all') or the ones in announcement_tenants
-- ('tenants').
-- Members see an announcement as a banner while it is published and inside
-- its window; channels optionally deliver it once through the notification
-- system (in-app, mail, sms, push) when it starts.
CREATE TABLE announcements (
    id           bigserial PRIMARY KEY,
    title        text NOT NULL,
    body         text,
    kind         text NOT NULL DEFAULT 'info',
    cta_label    text,
    cta_url      text,
    audience     text NOT NULL DEFAULT 'all',
    starts_at    timestamptz NOT NULL DEFAULT now(),
    ends_at      timestamptz,
    dismissible  boolean NOT NULL DEFAULT true,
    status       text NOT NULL DEFAULT 'published',
    channels     text[] NOT NULL DEFAULT '{}',
    delivered_at timestamptz,
    created_by   bigint,
    updated_by   bigint,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT announcements_kind_check CHECK (kind IN ('info', 'promotion', 'warning', 'critical')),
    CONSTRAINT announcements_audience_check CHECK (audience IN ('all', 'tenants')),
    CONSTRAINT announcements_status_check CHECK (status IN ('draft', 'published', 'archived')),
    CONSTRAINT announcements_window_check CHECK (ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT announcements_cta_check CHECK ((cta_label IS NULL) = (cta_url IS NULL))
);

CREATE INDEX announcements_live_idx ON announcements (status, starts_at, ends_at);
CREATE INDEX announcements_created_idx ON announcements (created_at DESC, id DESC);

CREATE TABLE announcement_tenants (
    announcement_id bigint NOT NULL REFERENCES announcements (id) ON DELETE CASCADE,
    tenant_id       bigint NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    PRIMARY KEY (announcement_id, tenant_id)
);

CREATE INDEX announcement_tenants_tenant_idx ON announcement_tenants (tenant_id);

-- What members did with an announcement, once per person and kind: the
-- first view, click and dismissal. Counts are unique people.
CREATE TABLE announcement_events (
    announcement_id bigint NOT NULL REFERENCES announcements (id) ON DELETE CASCADE,
    user_id         bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    tenant_id       bigint REFERENCES tenants (id) ON DELETE CASCADE,
    type            text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (announcement_id, user_id, type),
    CONSTRAINT announcement_events_type_check CHECK (type IN ('view', 'click', 'dismiss'))
);

CREATE INDEX announcement_events_tenant_idx ON announcement_events (announcement_id, tenant_id);
CREATE INDEX announcement_events_user_idx ON announcement_events (user_id);

-- +goose Down
DROP TABLE announcement_events;
DROP TABLE announcement_tenants;
DROP TABLE announcements;
