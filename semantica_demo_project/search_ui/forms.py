# File: search_ui/forms.py
from django import forms

class URLTaskForm(forms.Form):
    url = forms.URLField( # Django URLField сам валидирует URL
        label="URL для индексации",
        max_length=2000,
        widget=forms.URLInput(attrs={'class': 'form-control mb-2', 'placeholder': 'https://example.com/article'})
    )

class FileTaskForm(forms.Form):
    uploaded_file = forms.FileField(
        label="Выберите файл для индексации",
        required=True, # Сделали обязательным
        widget=forms.ClearableFileInput(attrs={'class': 'form-control mb-3'})
    )
    # Демо-поля ниже не нужны в форме, если они генерируются во view

class SearchForm(forms.Form):
    query_text = forms.CharField(
        label="Поисковый запрос",
        max_length=500,
        required=True,
        widget=forms.TextInput(attrs={'class': 'form-control form-control-lg mb-3', 'placeholder': 'Что вы хотите найти?'})
    )