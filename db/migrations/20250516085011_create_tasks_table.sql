-- +goose Up
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),                         -- Primary key, unique identifier for the task (UUID)
    user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,             -- Foreign key referencing the user who initiated the task (nullable)
    task_type VARCHAR(100) NOT NULL,                                       -- Type of the task (e.g., crawl_url, upload_file)
    status VARCHAR(100) NOT NULL,                                          -- Current status of the task in the processing pipeline
    
    input_url TEXT NULL,                                                   -- URL to be crawled, if applicable
    input_file_name VARCHAR(255) NULL,                                     -- Original name of the uploaded file, if applicable
    input_file_content_type VARCHAR(100) NULL,                             -- MIME type of the uploaded file, if applicable

    raw_data_path TEXT NULL,                                               -- Path/reference to the raw data artifact (e.g., in Minio)
    processed_data_path TEXT NULL,                                         -- Path/reference to the processed data artifact (e.g., Markdown, extracted text)
    
    error_message TEXT NULL,                                               -- Details of the error if the task failed

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),                         -- Timestamp of when the task was created
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),                         -- Timestamp of when the task was last updated
    completed_at TIMESTAMPTZ NULL                                          -- Timestamp of when the task was completed or failed
);

CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_task_type ON tasks(task_type);

COMMENT ON TABLE tasks IS 'Stores information about indexing tasks and their lifecycle';

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_task_type;
DROP INDEX IF EXISTS idx_tasks_status;
DROP INDEX IF EXISTS idx_tasks_user_id;
DROP TABLE IF EXISTS tasks;