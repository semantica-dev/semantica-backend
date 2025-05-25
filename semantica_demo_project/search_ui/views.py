# File: search_ui/views.py
from django.shortcuts import render, redirect
from django.urls import reverse
from django.contrib import messages
# requests и json больше не нужны для прямого взаимодействия с mock API
import uuid 
import datetime # Нужен для datetime объектов

from .forms import URLTaskForm, FileTaskForm, SearchForm
from . import mock_api_logic # Импортируем наш модуль с логикой

def dashboard_view(request):
    context = {
        'page_title': 'Главная панель'
    }
    return render(request, 'search_ui/dashboard.html', context)

def task_management_view(request):
    url_form = URLTaskForm(request.POST or None, prefix="url_task")
    file_form = FileTaskForm(request.POST or None, request.FILES or None, prefix="file_task") 
    
    if request.method == 'POST':
        task_id_from_form = None # Для сохранения ID только что созданной задачи
        task_action_successful = False

        if 'submit_url_task' in request.POST and url_form.is_valid():
            url_to_crawl = url_form.cleaned_data['url']
            try:
                response_data = mock_api_logic.logic_create_crawl_task(str(url_to_crawl)) # Убедимся, что передаем строку
                task_id_from_form = response_data.get("task_id")
                if task_id_from_form:
                    messages.success(request, f"Задача на индексацию URL '{url_to_crawl}' успешно создана (ID: {task_id_from_form}).")
                    task_action_successful = True
            except Exception as e: 
                messages.error(request, f"Ошибка при создании задачи URL: {e}")

        elif 'submit_file_task' in request.POST and file_form.is_valid():
            uploaded_file_obj = file_form.cleaned_data.get('uploaded_file')
            if uploaded_file_obj:
                original_name = uploaded_file_obj.name
                mock_minio_path = f"user-uploads/demo-user/{uuid.uuid4()}/{original_name}"
                try:
                    response_data = mock_api_logic.logic_create_file_task(mock_minio_path, original_name)
                    task_id_from_form = response_data.get("task_id")
                    if task_id_from_form:
                        messages.success(request, f"Задача на индексацию файла '{original_name}' успешно создана (ID: {task_id_from_form}). Имитация пути в MinIO: {mock_minio_path}")
                        task_action_successful = True
                except Exception as e:
                    messages.error(request, f"Ошибка при создании задачи для файла: {e}")
            else:
                messages.warning(request, "Пожалуйста, выберите файл для загрузки.")
        
        if task_action_successful and task_id_from_form:
            session_task_ids = request.session.get('task_ids', [])
            if task_id_from_form not in session_task_ids:
                 session_task_ids.insert(0, task_id_from_form)
            request.session['task_ids'] = session_task_ids # Обновляем список в сессии
            request.session.modified = True
            return redirect(reverse('search_ui:task_management')) # PRG pattern

    # Получение статусов для существующих задач из сессии для GET запроса или после неудачного POST
    tasks_with_status_display = []
    session_task_ids = request.session.get('task_ids', [])
    
    if session_task_ids:
        for task_id_in_session in session_task_ids: 
            task_status_data = mock_api_logic.logic_get_task_status(task_id_in_session)
            if task_status_data:
                # Преобразование datetime в строки ISO для передачи в шаблон
                for key_dt in ['created_at', 'updated_at', 'completed_at']:
                    if task_status_data.get(key_dt) and isinstance(task_status_data[key_dt], datetime.datetime):
                        task_status_data[key_dt] = task_status_data[key_dt].isoformat().replace('+00:00', 'Z')
                
                # Гарантируем наличие всех полей, ожидаемых шаблоном
                task_status_data.setdefault('original_file_name', None)
                task_status_data.setdefault('error_message', None)
                task_status_data.setdefault('task_type', 'N/A')
                task_status_data.setdefault('input_details', 'N/A')
                tasks_with_status_display.append(task_status_data)
            else: 
                tasks_with_status_display.append({
                    "task_id": task_id_in_session, "status": "не найдена",
                    "input_details": "N/A", "task_type": "N/A", "created_at": None, "updated_at": None, 
                    "original_file_name": None, "error_message": "Задача не найдена в mock DB", "completed_at": None
                })
    
    context = {
        'page_title': 'Управление задачами индексации',
        'url_form': url_form if request.method == 'GET' else URLTaskForm(prefix="url_task"), # Сбрасываем форму при ошибке POST или для GET
        'file_form': file_form if request.method == 'GET' else FileTaskForm(prefix="file_task"), # Сбрасываем форму
        'tasks': tasks_with_status_display,
    }
    return render(request, 'search_ui/task_management.html', context)

def search_view(request):
    search_form = SearchForm(request.GET or None) 
    search_results_display = None
    total_found_display = 0
    query_time_ms_display = 0
    performed_search = 'query_text' in request.GET and request.GET['query_text'] 

    if search_form.is_valid():
        query_text = search_form.cleaned_data['query_text']
        try:
            search_data = mock_api_logic.logic_execute_search(
                query_text=query_text,
                search_mode="hybrid", 
                limit=10,
                offset=0 
            )
            search_results_display = search_data.get("results", [])
            total_found_display = search_data.get("total_found", 0)
            query_time_ms_display = search_data.get("query_time_ms", 0)
            
            if not search_results_display and performed_search:
                messages.info(request, f"По вашему запросу '{query_text}' ничего не найдено.")
        except Exception as e:
            messages.error(request, f"Ошибка при выполнении поиска: {e}")
            search_results_display = []
    elif performed_search and not search_form.is_valid(): # Если был GET запрос с query_text, но форма невалидна
        messages.warning(request, "Пожалуйста, введите поисковый запрос.")

    context = {
        'page_title': 'Поиск информации',
        'search_form': search_form,
        'search_results': search_results_display,
        'total_found': total_found_display,
        'query_time_ms': query_time_ms_display,
        'performed_search': performed_search,
    }
    return render(request, 'search_ui/search_page.html', context)