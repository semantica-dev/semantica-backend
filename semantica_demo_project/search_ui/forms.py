# File: search_ui/forms.py
from django import forms

class URLTaskForm(forms.Form):
    url = forms.URLField(
        label="URL для индексации",
        max_length=2000,
        widget=forms.URLInput(attrs={'class': 'form-control', 'placeholder': 'https://example.com/article'})
    )

class FileTaskForm(forms.Form):
    uploaded_file = forms.FileField(
        label="Выберите файл для индексации",
        required=True,
        widget=forms.ClearableFileInput(attrs={'class': 'form-control'}) # Убедись, что form-control здесь есть
    )

class SearchForm(forms.Form):
    query_text = forms.CharField(
        label="Поисковый запрос",
        max_length=500,
        required=True,
        widget=forms.TextInput(attrs={'class': 'form-control form-control-lg', 'placeholder': 'Что вы хотите найти?'})
    )