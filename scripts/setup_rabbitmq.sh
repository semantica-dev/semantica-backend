#!/bin/bash

# Скрипт для настройки RabbitMQ: создание exchange, очередей и их привязок.
# Убедитесь, что у вас установлен rabbitmqadmin и он доступен в PATH,
# или укажите полный путь к нему.
# rabbitmqadmin можно скачать с вашего RabbitMQ сервера: http://localhost:15672/cli/rabbitmqadmin
# (требуется Python)

# Настройки подключения к RabbitMQ (если отличаются от стандартных)
# RABBITMQ_HOST="localhost"
# RABBITMQ_PORT="15672"
# RABBITMQ_VHOST="/"
# RABBITMQ_USER="guest"
# RABBITMQ_PASS="guest"
# RABBITMQ_ADMIN_CMD="rabbitmqadmin --host=$RABBITMQ_HOST --port=$RABBITMQ_PORT --vhost=$RABBITMQ_VHOST --username=$RABBITMQ_USER --password=$RABBITMQ_PASS"
RABBITMQ_ADMIN_CMD="rabbitmqadmin --host=localhost --port=15672 --vhost=/ --username=guest --password=guest"


EXCHANGE_NAME="tasks_exchange"

echo "-----------------------------------------------------"
echo "Setting up RabbitMQ entities for Semantica Backend..."
echo "Using Exchange: $EXCHANGE_NAME"
echo "-----------------------------------------------------"

# 1. Объявляем общий Exchange
echo ""
echo "[EXCHANGE] Declaring exchange: $EXCHANGE_NAME (type: topic, durable: true)"
$RABBITMQ_ADMIN_CMD declare exchange name=$EXCHANGE_NAME type=topic durable=true
if [ $? -ne 0 ]; then echo "Error declaring exchange $EXCHANGE_NAME"; exit 1; fi

# Вспомогательная функция для объявления очереди и ее привязки
declare_queue_and_bind() {
  local queue_name="$1"
  local routing_key="$2"
  local purpose="$3" # Описание назначения очереди

  echo ""
  echo "[QUEUE] $purpose: $queue_name"
  echo "  -> Declaring queue..."
  $RABBITMQ_ADMIN_CMD declare queue name=$queue_name durable=true
  if [ $? -ne 0 ]; then echo "Error declaring queue $queue_name"; exit 1; fi

  echo "  -> Binding to $EXCHANGE_NAME with routing key '$routing_key'..."
  $RABBITMQ_ADMIN_CMD declare binding source=$EXCHANGE_NAME destination=$queue_name routing_key=$routing_key
  if [ $? -ne 0 ]; then echo "Error binding queue $queue_name"; exit 1; fi
}

# --- 2. Очереди для ВОРКЕРОВ (входящие задачи от Оркестратора) ---
echo ""
echo "--- WORKER INPUT QUEUES (Tasks from Orchestrator) ---"
declare_queue_and_bind "tasks.crawl.in.queue" "crawl.task.in" "Crawler: Input Tasks"
declare_queue_and_bind "tasks.extract.html.in.queue" "extract.html.task.in" "ExtractorHTML: Input Tasks"
declare_queue_and_bind "tasks.extract.other.in.queue" "extract.other.task.in" "ExtractorOther: Input Tasks"
declare_queue_and_bind "tasks.index.keywords.in.queue" "index.keywords.task.in" "IndexerKeywords: Input Tasks"
declare_queue_and_bind "tasks.index.embeddings.in.queue" "index.embeddings.task.in" "IndexerEmbeddings: Input Tasks"

# --- 3. Очереди для ОРКЕСТРАТОРА (прослушивание результатов от Воркеров) ---
echo ""
echo "--- ORCHESTRATOR INPUT QUEUES (Results from Workers) ---"
declare_queue_and_bind "orchestrator.crawl.results.queue" "crawl.result.out" "Orchestrator: Crawler Results"
declare_queue_and_bind "orchestrator.extract_html.results.queue" "extract.html.result.out" "Orchestrator: ExtractorHTML Results"
declare_queue_and_bind "orchestrator.extract_other.results.queue" "extract.other.result.out" "Orchestrator: ExtractorOther Results"
declare_queue_and_bind "orchestrator.index_keywords.results.queue" "index.keywords.result.out" "Orchestrator: IndexerKeywords Results"
declare_queue_and_bind "orchestrator.index_embeddings.results.queue" "index.embeddings.result.out" "Orchestrator: IndexerEmbeddings Results"
declare_queue_and_bind "orchestrator.task_finished.queue" "task.processing.finished" "Orchestrator: Task Processing Finished Events"

# --- 4. Опциональные очереди для МОНИТОРИНГА (если нужны) ---
# Эти очереди могут использоваться для независимого наблюдения за потоком сообщений,
# не влияя на основные рабочие процессы. Они подписываются на те же routing keys,
# что и рабочие очереди результатов, если exchange это позволяет (например, topic).
# echo ""
# echo "--- (Optional) MONITORING QUEUES ---"
# declare_queue_and_bind "monitor.crawl.results.queue" "crawl.result.out" "Monitoring: Crawler Results"
# declare_queue_and_bind "monitor.extract_html.results.queue" "extract.html.result.out" "Monitoring: ExtractorHTML Results"
# ... и так далее для других результатов ...
# declare_queue_and_bind "monitor.task_finished.queue" "task.processing.finished" "Monitoring: Task Processing Finished Events"

echo ""
echo "-----------------------------------------------------"
echo "RabbitMQ setup complete."
echo "-----------------------------------------------------"