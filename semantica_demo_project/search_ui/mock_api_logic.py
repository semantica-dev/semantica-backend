# File: search_ui/mock_api_logic.py
import uuid
import datetime
import random
from typing import List, Optional, Dict, Any
from pydantic import BaseModel, HttpUrl

# --- "База данных" в памяти для задач ---
tasks_db: Dict[str, Dict[str, Any]] = {}
MOCK_USER_ID = "semantica-admin"

# --- Mock данные и логика ---
status_cycle = [
    "pending",
    "processing_collect",
    "processing_extraction",
    "processing_indexing_keywords",
    "processing_indexing_embeddings",
    "completed_successfully"
]
status_error_message = "В процессе обработки произошла непредвиденная ошибка."

class SearchResultItemLogic(BaseModel):
    document_id: str
    title: str
    url: Optional[HttpUrl] = None
    original_file_name: Optional[str] = None
    snippet: str
    relevance_score: float
    source_type: str

mock_search_results_data = [
    SearchResultItemLogic(
        document_id="d290f1ee-6c54-4b01-90e6-d701748f0851",
        title="Семантический поиск (homemade)",
        url=HttpUrl("https://habr.com/ru/articles/834356/"),
        original_file_name=None,
        snippet="Основой семантического поиска может являться ML задача *Sentence Similarity*, а если быть еще конкретнее, то это *Semantic Textual Similarity*.",
        relevance_score=0.95, # Число
        source_type="url"
    ),
    SearchResultItemLogic(
        document_id="b2c3d4e5-f6a7-8901-2345-67890abcdef1",
        title="Обработка естественного языка (NLP) методами машинного обучения в Python",
        url=HttpUrl("https://habr.com/ru/companies/otus/articles/687796/"),
        original_file_name=None,
        snippet="В данной статье хотелось бы рассказать о том, как можно применить различные методы машинного обучения (ML) для обработки текста, чтобы можно было произвести его бинарную классифицию.",
        relevance_score=0.82, # Число
        source_type="url"
    ),
    SearchResultItemLogic(
        document_id="a1b2c3d4-e5f6-7890-1234-567890abcde0",
        title="Принципы работы нейронных сетей",
        url=None, # HttpUrl может быть None
        original_file_name="neural_networks_intro.pdf",
        snippet="Основные **принципы работы** глубоких нейронных сетей и их применение в **искусственном интеллекте**.",
        relevance_score=0.76, # Число
        source_type="file_upload"
    ),
]


def get_next_status_logic(current_status: Optional[str]) -> str:
    if not current_status or current_status == "completed_successfully" or current_status == "failed":
        return status_cycle[0]
    try:
        idx = status_cycle.index(current_status)
        if idx == len(status_cycle) - 1: 
            return status_cycle[idx] 
        return status_cycle[(idx + 1)]
    except ValueError:
        return status_cycle[0]

def logic_create_crawl_task(url: str) -> Dict[str, str]:
    task_id = str(uuid.uuid4())
    now = datetime.datetime.now(datetime.timezone.utc)
    tasks_db[task_id] = {
        "user_id": MOCK_USER_ID,
        "status": status_cycle[0],
        "task_type": "crawl_url",
        "input_details": str(url),
        "original_file_name": None,
        "error_message": None,
        "created_at": now,
        "updated_at": now,
        "completed_at": None,
        "status_request_count": 0
    }
    return {"task_id": task_id}

def logic_create_file_task(file_minio_path: str, original_file_name: Optional[str]) -> Dict[str, str]:
    task_id = str(uuid.uuid4())
    now = datetime.datetime.now(datetime.timezone.utc)
    tasks_db[task_id] = {
        "user_id": MOCK_USER_ID,
        "status": status_cycle[0],
        "task_type": "upload_file",
        "input_details": file_minio_path,
        "original_file_name": original_file_name,
        "error_message": None,
        "created_at": now,
        "updated_at": now,
        "completed_at": None,
        "status_request_count": 0
    }
    return {"task_id": task_id}

def logic_get_task_status(task_id: str) -> Optional[Dict[str, Any]]:
    task_data = tasks_db.get(task_id)
    if not task_data:
        return None

    task_data["status_request_count"] += 1
    if task_data["status"] not in ["completed_successfully", "failed"]:
        if task_data["status_request_count"] % 2 == 1 or task_data["status"] == "pending": 
            task_data["status"] = get_next_status_logic(task_data["status"])
            task_data["updated_at"] = datetime.datetime.now(datetime.timezone.utc)
            if task_data["status"] == "completed_successfully":
                task_data["completed_at"] = task_data["updated_at"]
            elif random.random() < 0.1 and task_data["status"] != status_cycle[-1]: 
                task_data["status"] = "failed"
                task_data["error_message"] = status_error_message
                task_data["completed_at"] = task_data["updated_at"]
    
    response_data = task_data.copy()
    response_data["task_id"] = task_id 
    return response_data

def logic_execute_search(query_text: str, search_mode: str, limit: int, offset: int) -> Dict[str, Any]:
    start = offset
    end = offset + limit
    
    current_results_objects = []
    if "семантич" in query_text.lower():
        current_results_objects = mock_search_results_data
    elif "нейрон" in query_text.lower():
        current_results_objects = [mock_search_results_data[2], mock_search_results_data[0]]
    elif "python" in query_text.lower():
        current_results_objects = [mock_search_results_data[1]]
    else: 
        current_results_objects = [mock_search_results_data[0], mock_search_results_data[1]] 

    paginated_results = current_results_objects[start:end]
    
    paginated_results_dicts = []
    for item in paginated_results:
        item_dict = item.model_dump() # Получаем словарь из Pydantic модели
        # Убедимся, что score это float, а не строка
        if "relevance_score" in item_dict and not isinstance(item_dict["relevance_score"], (int, float)):
            try:
                item_dict["relevance_score"] = float(item_dict["relevance_score"])
            except (ValueError, TypeError):
                item_dict["relevance_score"] = 0.0 
        elif "relevance_score" not in item_dict:
             item_dict["relevance_score"] = 0.0
        
        # Преобразуем HttpUrl в строку для шаблона, если оно есть
        if item_dict.get("url") is not None: # Проверяем что не None
            item_dict["url"] = str(item_dict["url"])
        else:
            item_dict["url"] = None # Явно устанавливаем None, если было None
            
        paginated_results_dicts.append(item_dict)
    
    return {
        "results": paginated_results_dicts,
        "total_found": len(current_results_objects),
        "query_time_ms": random.randint(50, 300)
    }