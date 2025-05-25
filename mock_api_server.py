import uuid
from fastapi import FastAPI, HTTPException, Body, Path
from pydantic import BaseModel, Field, HttpUrl
from typing import List, Optional, Dict, Any
import datetime
import random

# --- Pydantic модели на основе OpenAPI спецификаций ---

# Orchestrator Models
class CrawlTaskRequest(BaseModel):
    url: HttpUrl

class FileTaskRequest(BaseModel):
    file_minio_path: str
    original_file_name: Optional[str] = None

class TaskAcceptedResponse(BaseModel):
    task_id: str

class TaskStatusResponse(BaseModel):
    task_id: str
    user_id: str
    status: str
    task_type: str
    input_details: str
    error_message: Optional[str] = None
    created_at: datetime.datetime
    updated_at: datetime.datetime
    completed_at: Optional[datetime.datetime] = None

# Search Models
class SearchQueryRequest(BaseModel):
    query_text: str
    search_mode: Optional[str] = "hybrid"
    limit: Optional[int] = 10
    offset: Optional[int] = 0

class SearchResultItem(BaseModel):
    document_id: str
    title: str
    url: Optional[HttpUrl] = None
    original_file_name: Optional[str] = None
    snippet: str
    relevance_score: float
    source_type: str

class SearchResponse(BaseModel):
    results: List[SearchResultItem]
    total_found: int
    query_time_ms: int

class ErrorResponse(BaseModel):
    error_code: str
    message: str

# --- FastAPI приложение ---
app = FastAPI(title="Mock API Server for Semantica")

# --- "База данных" в памяти для задач ---
tasks_db: Dict[str, Dict[str, Any]] = {}
MOCK_USER_ID = "semantica-admin"

# --- Mock данные и логика ---

# Статусы для циркуляции
status_cycle = [
    "pending",
    "processing_collect",
    "processing_extraction",
    "processing_indexing_keywords",
    "processing_indexing_embeddings",
    "completed_successfully"
]
status_error_message = "An unexpected error occurred during processing."

def get_next_status(current_status: Optional[str]) -> str:
    if not current_status or current_status == "completed_successfully" or current_status == "failed":
        return status_cycle[0]
    try:
        idx = status_cycle.index(current_status)
        return status_cycle[(idx + 1) % len(status_cycle)]
    except ValueError:
        return status_cycle[0]

# --- API Оркестратора ---

@app.post("/api/v1/tasks/crawl", response_model=TaskAcceptedResponse, status_code=202)
async def create_crawl_task(request: CrawlTaskRequest):
    task_id = str(uuid.uuid4())
    now = datetime.datetime.now(datetime.timezone.utc)
    tasks_db[task_id] = {
        "user_id": MOCK_USER_ID,
        "status": status_cycle[0],
        "task_type": "crawl_url",
        "input_details": str(request.url),
        "error_message": None,
        "created_at": now,
        "updated_at": now,
        "completed_at": None,
        "status_request_count": 0 # Для симуляции изменения статуса
    }
    return TaskAcceptedResponse(task_id=task_id)

@app.post("/api/v1/tasks/file", response_model=TaskAcceptedResponse, status_code=202)
async def create_file_task(request: FileTaskRequest):
    task_id = str(uuid.uuid4())
    now = datetime.datetime.now(datetime.timezone.utc)
    tasks_db[task_id] = {
        "user_id": MOCK_USER_ID,
        "status": status_cycle[0],
        "task_type": "upload_file",
        "input_details": request.file_minio_path,
        "error_message": None,
        "created_at": now,
        "updated_at": now,
        "completed_at": None,
        "status_request_count": 0
    }
    return TaskAcceptedResponse(task_id=task_id)

@app.get("/api/v1/tasks/{task_id}/status", response_model=TaskStatusResponse)
async def get_task_status(task_id: str = Path(..., title="Task ID")):
    task_data = tasks_db.get(task_id) # Переименовал для ясности
    if not task_data:
        raise HTTPException(status_code=404, detail={"error_code": "TASK_NOT_FOUND", "message": "Task not found"})

    # Симуляция изменения статуса при каждом запросе
    task_data["status_request_count"] += 1
    
    if task_data["status_request_count"] % 2 == 1 or task_data["status"] == "pending":
        if task_data["status"] not in ["completed_successfully", "failed"]:
            task_data["status"] = get_next_status(task_data["status"])
            task_data["updated_at"] = datetime.datetime.now(datetime.timezone.utc)
            if task_data["status"] == "completed_successfully":
                task_data["completed_at"] = task_data["updated_at"]
            elif random.random() < 0.1 and task_data["status"] != "completed_successfully":
                task_data["status"] = "failed"
                task_data["error_message"] = status_error_message
                task_data["completed_at"] = task_data["updated_at"]

    # !!! ВОТ ИСПРАВЛЕНИЕ: добавляем task_id в словарь перед созданием объекта Pydantic !!!
    response_data = task_data.copy() # Копируем, чтобы не изменять исходный tasks_db случайно
    response_data["task_id"] = task_id # Добавляем сам task_id в данные для ответа

    return TaskStatusResponse(**response_data)


# --- API Поискового сервиса ---

mock_search_results = [
    SearchResultItem(
        document_id="d290f1ee-6c54-4b01-90e6-d701748f0851",
        title="Семантический поиск (homemade)",
        url=HttpUrl("https://habr.com/ru/articles/834356/"),
        original_file_name=None,
        snippet="Основой семантического поиска может являться ML задача *Sentence Similarity*, а если быть еще конкретнее, то это *Semantic Textual Similarity*.",
        relevance_score=0.95,
        source_type="url"
    ),
    SearchResultItem(
        document_id="b2c3d4e5-f6a7-8901-2345-67890abcdef1",
        title="Обработка естественного языка (NLP) методами машинного обучения в Python",
        url=HttpUrl("https://habr.com/ru/companies/otus/articles/687796/"),
        original_file_name=None,
        snippet="В данной статье хотелось бы рассказать о том, как можно применить различные методы машинного обучения (ML) для обработки текста, чтобы можно было произвести его бинарную классифицию.",
        relevance_score=0.82,
        source_type="url"
    ),
    SearchResultItem(
        document_id="a1b2c3d4-e5f6-7890-1234-567890abcde0",
        title="Принципы работы нейронных сетей",
        url=None,
        original_file_name="neural_networks_intro.pdf",
        snippet="Основные **принципы работы** глубоких нейронных сетей и их применение в **искусственном интеллекте**.",
        relevance_score=0.76,
        source_type="file_upload"
    ),
]

@app.post("/api/v1/search", response_model=SearchResponse)
async def execute_search(request: SearchQueryRequest):
    # Просто возвращаем несколько результатов из mock_search_results,
    # немного "перемешивая" их для разнообразия или обрезая по limit/offset
    
    start = request.offset
    end = request.offset + request.limit
    
    # Простая симуляция поиска: если в запросе есть "семантич", даем больше результатов
    current_results = []
    if "семантич" in request.query_text.lower():
        current_results = mock_search_results
    elif "нейрон" in request.query_text.lower():
        current_results = [mock_search_results[1], mock_search_results[0]]
    else:
        current_results = [mock_search_results[2]]

    paginated_results = current_results[start:end]
    
    return SearchResponse(
        results=paginated_results,
        total_found=len(current_results),
        query_time_ms=random.randint(50, 300) # Случайное время ответа
    )

# --- Запуск сервера (если файл запускается напрямую) ---
if __name__ == "__main__":
    import uvicorn
    # Запускаем на порту 8000, так как порты 8080 и 8081 уже "заняты" в наших спецификациях
    # для Оркестратора и Поискового сервиса соответственно.
    # Для мока можно использовать любой свободный порт.
    uvicorn.run(app, host="127.0.0.1", port=8000)