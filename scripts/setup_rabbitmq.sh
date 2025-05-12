#!/bin/bash

# Убедитесь, что rabbitmqadmin доступен и настроен (может потребоваться указать --host, --port, --vhost, --username, --password)
# RABBITMQ_ADMIN="python /path/to/rabbitmqadmin"
RABBITMQ_ADMIN="./scripts/rabbitmqadmin --host=localhost --port=15672 --vhost=/ --username=guest --password=guest"

# Общий Exchange
echo "Declaring exchange: tasks_exchange"
$RABBITMQ_ADMIN declare exchange name=tasks_exchange type=topic durable=true

# Очередь для входящих задач краулинга
QUEUE_CRAWL_IN="tasks.crawl.in.queue"
ROUTING_KEY_CRAWL_IN="crawl.task.in"
echo "Declaring queue: $QUEUE_CRAWL_IN"
$RABBITMQ_ADMIN declare queue name=$QUEUE_CRAWL_IN durable=true
echo "Binding queue $QUEUE_CRAWL_IN to tasks_exchange with routing key $ROUTING_KEY_CRAWL_IN"
$RABBITMQ_ADMIN declare binding source=tasks_exchange destination=$QUEUE_CRAWL_IN routing_key=$ROUTING_KEY_CRAWL_IN

# Очередь для исходящих результатов краулинга (для наблюдения)
QUEUE_CRAWL_OUT="tasks.crawl.out.queue" # Для наблюдения, наш код пока не слушает эту очередь
ROUTING_KEY_CRAWL_OUT="crawl.result.out"
echo "Declaring queue: $QUEUE_CRAWL_OUT"
$RABBITMQ_ADMIN declare queue name=$QUEUE_CRAWL_OUT durable=true
echo "Binding queue $QUEUE_CRAWL_OUT to tasks_exchange with routing key $ROUTING_KEY_CRAWL_OUT"
$RABBITMQ_ADMIN declare binding source=tasks_exchange destination=$QUEUE_CRAWL_OUT routing_key=$ROUTING_KEY_CRAWL_OUT

echo "RabbitMQ setup complete."

# Вы можете также создать эти сущности вручную через RabbitMQ Management UI:
# 1. Exchanges -> Add a new exchange:
#    Name: tasks_exchange, Type: topic, Durability: Durable
# 2. Queues -> Add a new queue:
#    Name: tasks.crawl.in.queue, Durability: Durable
# 3. Queues -> Add a new queue:
#    Name: tasks.crawl.out.queue, Durability: Durable
# 4. Нажмите на exchange 'tasks_exchange', перейдите в 'Bindings'.
#    - Bind 'tasks.crawl.in.queue' to 'tasks_exchange' with routing key 'crawl.task.in'
#    - Bind 'tasks.crawl.out.queue' to 'tasks_exchange' with routing key 'crawl.result.out'