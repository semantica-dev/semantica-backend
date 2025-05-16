// File: pkg/messaging/events.go
package messaging

const (
	TasksExchange = "tasks_exchange" // Общий exchange для задач

	// --- События для Краулера ---
	CrawlTaskRoutingKey   = "crawl.task.in"    // Роутинг ключ для входящих задач краулинга
	CrawlResultRoutingKey = "crawl.result.out" // Роутинг ключ для исходящих результатов краулинга
)

type CrawlTaskEvent struct {
	TaskID string `json:"task_id"`
	URL    string `json:"url"`
}

type CrawlResultEvent struct {
	TaskID      string `json:"task_id"`
	URL         string `json:"url"`
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	RawDataPath string `json:"raw_data_path,omitempty"` // Путь к сырым данным в Minio (добавим позже реальное значение)
}

// --- События для Экстрактора HTML ---
const (
	ExtractHTMLTaskRoutingKey   = "extract.html.task.in"
	ExtractHTMLResultRoutingKey = "extract.html.result.out"
)

type ExtractHTMLTaskEvent struct {
	TaskID      string `json:"task_id"`
	OriginalURL string `json:"original_url"`  // URL исходной страницы
	RawDataPath string `json:"raw_data_path"` // Путь к сырым данным в Minio (результат краулера)
}

type ExtractHTMLResultEvent struct {
	TaskID       string `json:"task_id"`
	OriginalURL  string `json:"original_url"`
	MarkdownPath string `json:"markdown_path,omitempty"` // Путь к Markdown в Minio (добавим позже реальное значение)
	Success      bool   `json:"success"`
	Message      string `json:"message,omitempty"`
}

// --- События для Экстрактора Других Файлов (PDF, DOCX, TXT) ---
const (
	ExtractOtherTaskRoutingKey   = "extract.other.task.in"
	ExtractOtherResultRoutingKey = "extract.other.result.out"
)

type ExtractOtherTaskEvent struct {
	TaskID           string `json:"task_id"`
	OriginalFilePath string `json:"original_file_path"` // Имя загруженного файла или URL
	RawDataPath      string `json:"raw_data_path"`      // Путь к сырому файлу в Minio
}

type ExtractOtherResultEvent struct {
	TaskID            string `json:"task_id"`
	OriginalFilePath  string `json:"original_file_path"`
	ExtractedTextPath string `json:"extracted_text_path,omitempty"` // Путь к извлеченному тексту в Minio
	Success           bool   `json:"success"`
	Message           string `json:"message,omitempty"`
}

// --- События для Индексатора Ключевых Слов ---
const (
	IndexKeywordsTaskRoutingKey   = "index.keywords.task.in"
	IndexKeywordsResultRoutingKey = "index.keywords.result.out"
)

type IndexKeywordsTaskEvent struct {
	TaskID            string `json:"task_id"`
	OriginalURL       string `json:"original_url,omitempty"`       // Если источник - сайт
	OriginalFilePath  string `json:"original_file_path,omitempty"` // Если источник - файл
	ProcessedDataPath string `json:"processed_data_path"`          // Путь к Markdown или извлеченному тексту в Minio
}

type IndexKeywordsResultEvent struct {
	TaskID           string `json:"task_id"`
	OriginalURL      string `json:"original_url,omitempty"`
	OriginalFilePath string `json:"original_file_path,omitempty"`
	KeywordsStored   bool   `json:"keywords_stored"` // Признак, что ключевые слова сохранены (пока bool)
	Success          bool   `json:"success"`
	Message          string `json:"message,omitempty"`
}

// --- События для Индексатора Эмбеддингов ---
const (
	IndexEmbeddingsTaskRoutingKey   = "index.embeddings.task.in"
	IndexEmbeddingsResultRoutingKey = "index.embeddings.result.out"
)

type IndexEmbeddingsTaskEvent struct {
	TaskID            string `json:"task_id"`
	OriginalURL       string `json:"original_url,omitempty"`
	OriginalFilePath  string `json:"original_file_path,omitempty"`
	ProcessedDataPath string `json:"processed_data_path"` // Путь к Markdown или извлеченному тексту
	// В будущем: информация о чанках, если разбиение происходит до этого воркера
}

type IndexEmbeddingsResultEvent struct {
	TaskID           string `json:"task_id"`
	OriginalURL      string `json:"original_url,omitempty"`
	OriginalFilePath string `json:"original_file_path,omitempty"`
	EmbeddingsStored bool   `json:"embeddings_stored"` // Признак, что эмбеддинги сохранены
	Success          bool   `json:"success"`
	Message          string `json:"message,omitempty"`
}

// --- Общее событие для завершения всей цепочки задач (опционально, для Оркестратора) ---
const (
	TaskProcessingFinishedRoutingKey = "task.processing.finished"
)

type TaskProcessingFinishedEvent struct {
	TaskID           string `json:"task_id"`
	OriginalURL      string `json:"original_url,omitempty"`
	OriginalFilePath string `json:"original_file_path,omitempty"`
	OverallSuccess   bool   `json:"overall_success"`
	FinalMessage     string `json:"final_message,omitempty"`
	// Сюда можно добавить ссылки на все артефакты, если нужно
}
