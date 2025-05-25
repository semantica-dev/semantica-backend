# File: search_ui/forms.py
from django import forms

class URLTaskForm(forms.Form):
    url = forms.URLField(
        label="URL для индексации",
        max_length=2000,
        widget=forms.URLInput(attrs={'class': 'form-control mb-2', 'placeholder': 'https://example.com/article'})
    )

class FileTaskForm(forms.Form):
    uploaded_file = forms.FileField(
        label="Выберите файл для индексации",
        required=True,
        widget=forms.ClearableFileInput(attrs={'class': 'form-control mb-3'})
    )

class SearchForm(forms.Form):
    query_text = forms.CharField(
        label="Поисковый запрос",
        max_length=500,
        widget=forms.TextInput(attrs={'class': 'form-control form-control-lg', 'placeholder': 'Введите ваш запрос...'})
    )
    # Опциональные поля, если мы хотим дать пользователю выбор
    # search_mode = forms.ChoiceField(
    #     label="Режим поиска",
    #     choices=[('hybrid', 'Гибридный'), ('semantic_only', 'Семантический'), ('keyword_only', 'По ключевым словам')],
    #     required=False,
    #     widget=forms.Select(attrs={'class': 'form-select mt-2'})
    # )