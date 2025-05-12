// File: internal/orchestrator/api/handler.go
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/semantica-dev/semantica-backend/internal/orchestrator/publisher" // Убедитесь, что путь правильный
	"github.com/semantica-dev/semantica-backend/pkg/messaging"                   // Убедитесь, что путь правильный
)

type CreateTaskRequest struct {
	URL string `json:"url"`
}

type CreateTaskResponse struct {
	TaskID string `json:"task_id"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type TaskAPIHandler struct {
	logger    *slog.Logger
	publisher *publisher.TaskPublisher
}

func NewTaskAPIHandler(logger *slog.Logger, pub *publisher.TaskPublisher) *TaskAPIHandler {
	return &TaskAPIHandler{
		logger:    logger.With("component", "task_api_handler"),
		publisher: pub,
	}
}

func (h *TaskAPIHandler) CreateCrawlTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request body", "error", err)
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	defer r.Body.Close()

	if req.URL == "" {
		h.logger.Warn("URL is empty in request")
		respondWithError(w, http.StatusBadRequest, "URL cannot be empty")
		return
	}

	taskID := uuid.NewString()
	h.logger.Info("Received new crawl task request", "url", req.URL, "assigned_task_id", taskID)

	crawlTask := messaging.CrawlTaskEvent{
		TaskID: taskID,
		URL:    req.URL,
	}

	// Используем context.Background() для MVP, в реальном приложении можно использовать context из запроса
	// или создать новый с таймаутом.
	if err := h.publisher.PublishCrawlTask(context.Background(), crawlTask); err != nil {
		h.logger.Error("Failed to publish crawl task", "error", err, "task_id", taskID)
		respondWithError(w, http.StatusInternalServerError, "Failed to submit task for processing")
		return
	}

	h.logger.Info("Crawl task published successfully", "task_id", taskID, "url", req.URL)
	respondWithJSON(w, http.StatusAccepted, CreateTaskResponse{TaskID: taskID})
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, ErrorResponse{Error: message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		// Если не можем замаршалить наш собственный ответ, это серьезная проблема
		slog.Default().Error("Failed to marshal JSON response", "error", err, "payload", payload) // Используем дефолтный логгер, т.к. наш мог не инициализироваться
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Failed to marshal JSON response"}`)) // Отдаем сырой JSON
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
