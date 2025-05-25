# File: search_ui/views.py
from django.shortcuts import render, redirect
from django.urls import reverse
from django.contrib import messages # Для flash-сообщений
import requests # Для HTTP-запросов к mock API
import json
import uuid # Для генерации mock_minio_path

from .forms import URLTaskForm, FileTaskForm, SearchForm # Импортируем все наши формы

# Адрес нашего mock API сервера
MOCK_API_BASE_URL = "http://127.0.0.1:8000/api/v1" # Убедись, что порт верный для твоего mock API

def dashboard_view(request):
    context = {
        'page_title': 'Главная панель'
    }
    return render(request, 'search_ui/dashboard.html', context)

def task_management_view(request):
    # Передаем request.FILES в FileTaskForm для обработки загрузки файлов
    url_form = URLTaskForm(request.POST or None, prefix="url_task")
    file_form = FileTaskForm(request.POST or None, request.FILES or None, prefix="file_task") 
    
    if request.method == 'POST':
        if 'submit_url_task' in request.POST and url_form.is_valid():
            try:
                api_url = f"{MOCK_API_BASE_URL}/tasks/crawl"
                payload = {"url": url_form.cleaned_data['url']}
                response = requests.post(api_url, json=payload, timeout=5)
                response.raise_for_status() # Вызовет исключение для 4xx/5xx ответов
                
                data = response.json()
                task_id = data.get("task_id")
                
                # Сохраняем task_id в сессию для отображения списка задач
                if 'task_ids' not in request.session:
                    request.session['task_ids'] = []
                # Добавляем в начало списка, чтобы новые задачи были наверху
                if task_id not in request.session['task_ids']:
                     request.session['task_ids'].insert(0, task_id) 
                request.session.modified = True # Важно для сохранения изменений в сессии

                messages.success(request, f"Задача на индексацию URL '{payload['url']}' успешно создана (ID: {task_id}).")
                return redirect(reverse('search_ui:task_management')) # Редирект для сброса POST
            except requests.exceptions.Timeout:
                messages.error(request, "Ошибка: Таймаут при обращении к API Оркестратора.")
            except requests.exceptions.RequestException as e:
                messages.error(request, f"Ошибка при создании задачи URL: {e}")
            except json.JSONDecodeError:
                messages.error(request, "Ошибка: Не удалось обработать ответ от API Оркестратора.")

        elif 'submit_file_task' in request.POST and file_form.is_valid():
            uploaded_file_obj = file_form.cleaned_data.get('uploaded_file')

            if uploaded_file_obj: # Проверяем, что файл действительно был выбран
                original_name = uploaded_file_obj.name
                # Имитируем путь в MinIO (в реальном приложении здесь была бы загрузка)
                mock_minio_path = f"user-uploads/demo-user/{uuid.uuid4()}/{original_name}"
                
                try:
                    api_url = f"{MOCK_API_BASE_URL}/tasks/file"
                    payload = {
                        "file_minio_path": mock_minio_path,
                        "original_file_name": original_name
                    }
                    response = requests.post(api_url, json=payload, timeout=5)
                    response.raise_for_status()
                    
                    data = response.json()
                    task_id = data.get("task_id")

                    if 'task_ids' not in request.session:
                        request.session['task_ids'] = []
                    if task_id not in request.session['task_ids']:
                         request.session['task_ids'].insert(0, task_id)
                    request.session.modified = True

                    messages.success(request, f"Задача на индексацию файла '{original_name}' успешно создана (ID: {task_id}). Имитация пути в MinIO: {mock_minio_path}")
                    return redirect(reverse('search_ui:task_management')) # Редирект для сброса POST
                except requests.exceptions.Timeout:
                    messages.error(request, "Ошибка: Таймаут при обращении к API Оркестратора.")
                except requests.exceptions.RequestException as e:
                    messages.error(request, f"Ошибка при создании задачи для файла: {e}")
                except json.JSONDecodeError:
                    messages.error(request, "Ошибка: Не удалось обработать ответ от API Оркестратора.")
            else:
                # Это условие не должно сработать, если uploaded_file required=True в форме
                messages.warning(request, "Пожалуйста, выберите файл для загрузки.")
        # Если ни одна форма не была валидна или не была отправлена нужная кнопка,
        # просто переходим к отображению страницы с формами (и, возможно, ошибками валидации)

    # Получение статусов для существующих задач из сессии
    tasks_with_status = []
    session_task_ids = request.session.get('task_ids', [])
    
    if session_task_ids:
        for task_id_in_session in session_task_ids: # Отображаем в том порядке, как добавляли (новые вверху)
            try:
                status_api_url = f"{MOCK_API_BASE_URL}/tasks/{task_id_in_session}/status"
                response = requests.get(status_api_url, timeout=3) 
                response.raise_for_status()
                task_data = response.json()
                # Убедимся, что все ключи, ожидаемые в шаблоне, присутствуют
                task_data.setdefault('original_file_name', None)
                task_data.setdefault('error_message', None)
                task_data.setdefault('task_type', 'N/A')
                task_data.setdefault('input_details', 'N/A')
                task_data.setdefault('created_at', None) # Шаблон обработает None
                task_data.setdefault('updated_at', None)
                task_data.setdefault('completed_at', None)
                tasks_with_status.append(task_data)
            except requests.exceptions.Timeout:
                 tasks_with_status.append({
                    "task_id": task_id_in_session, "status": "таймаут получения статуса",
                    "input_details": "N/A", "task_type": "N/A", "created_at": None, "updated_at": None, 
                    "original_file_name": None, "error_message": "Таймаут запроса к API статуса", "completed_at": None
                })
            except requests.exceptions.RequestException as e:
                tasks_with_status.append({
                    "task_id": task_id_in_session, "status": "ошибка получения статуса",
                    "input_details": "N/A", "task_type": "N/A", "created_at": None, "updated_at": None,
                    "error_message": str(e), "original_file_name": None, "completed_at": None
                })
            except json.JSONDecodeError:
                 tasks_with_status.append({
                    "task_id": task_id_in_session, "status": "ошибка ответа API статуса",
                    "input_details": "N/A", "task_type": "N/A", "created_at": None, "updated_at": None,
                    "original_file_name": None, "error_message": "Некорректный JSON в ответе API статуса", "completed_at": None
                })

    context = {
        'page_title': 'Управление задачами индексации',
        'url_form': url_form,
        'file_form': file_form,
        'tasks': tasks_with_status,
    }
    return render(request, 'search_ui/task_management.html', context)

def search_view(request):
    # Используем request.GET, чтобы параметры поиска были в URL
    search_form = SearchForm(request.GET or None) 
    search_results = None
    total_found = 0
    query_time_ms = 0
    # Флаг, чтобы понимать, был ли выполнен поиск (даже если результатов нет)
    performed_search = 'query_text' in request.GET and request.GET['query_text'] 

    if search_form.is_valid():
        query_text = search_form.cleaned_data['query_text']
        # performed_search уже установлен выше
        try:
            api_url = f"{MOCK_API_BASE_URL}/search"
            payload = {
                "query_text": query_text,
                "search_mode": "hybrid", 
                "limit": 10, 
                "offset": 0 
            }
            response = requests.post(api_url, json=payload, timeout=10)
            response.raise_for_status()
            data = response.json()
            search_results = data.get("results", [])
            total_found = data.get("total_found", 0)
            query_time_ms = data.get("query_time_ms", 0)
            
            if not search_results and performed_search: # Если поиск был, но ничего не найдено
                messages.info(request, f"По вашему запросу '{query_text}' ничего не найдено.")

        except requests.exceptions.Timeout:
            messages.error(request, "Ошибка: Таймаут при обращении к API Поиска.")
            search_results = [] 
        except requests.exceptions.RequestException as e:
            messages.error(request, f"Ошибка при выполнении поиска: {e}")
            search_results = []
        except json.JSONDecodeError:
            messages.error(request, "Ошибка: Не удалось обработать ответ от API Поиска.")
            search_results = []
    elif performed_search and not search_form.is_valid(): # Если был GET запрос с query_text, но форма невалидна (например, пустое поле)
        messages.warning(request, "Пожалуйста, введите поисковый запрос.")


    context = {
        'page_title': 'Поиск информации',
        'search_form': search_form,
        'search_results': search_results,
        'total_found': total_found,
        'query_time_ms': query_time_ms,
        'performed_search': performed_search, # Передаем флаг в шаблон
    }
    return render(request, 'search_ui/search_page.html', context)