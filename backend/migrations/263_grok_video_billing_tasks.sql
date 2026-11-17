-- New asynchronous video tasks retain their original billing identity even
-- after Redis expiry. No user credentials or request prompts are stored.
CREATE TABLE grok_video_billing_tasks (
 task_id TEXT NOT NULL,
 user_id BIGINT NOT NULL,
 api_key_id BIGINT NOT NULL,
 snapshot JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (user_id, api_key_id, task_id),
 CHECK (user_id > 0 AND api_key_id > 0 AND length(task_id) BETWEEN 1 AND 512),
 CHECK (jsonb_typeof(snapshot) = 'object' AND snapshot ? 'subscription_binding')
);
