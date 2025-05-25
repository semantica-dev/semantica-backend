from django.contrib import admin
from django.urls import path, include # Убедись, что include импортирован

urlpatterns = [
    path('admin/', admin.site.urls),
    path('', include('search_ui.urls')), # Подключаем URL-ы нашего приложения search_ui
]