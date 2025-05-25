# File: search_ui/urls.py
from django.urls import path
from . import views

app_name = 'search_ui'

urlpatterns = [
    path('', views.search_view, name='search_page'), # <--- ИЗМЕНЕНО: Теперь это главная страница
    path('dashboard/', views.dashboard_view, name='dashboard'), # <--- Дашборд теперь здесь
    path('tasks/', views.task_management_view, name='task_management'),
    # search_page_placeholder больше не нужен, так как search_view его заменяет
]