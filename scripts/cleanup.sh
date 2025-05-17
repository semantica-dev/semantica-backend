#!/bin/bash

# Скрипт для очистки и диагностики Docker-окружения проекта Semantica Backend
#
# Опции:
#   --list:    Показывает текущее состояние Docker (контейнеры, образы, тома, сети, использование диска).
#   --volumes: Удаляет именованные тома, связанные с проектом.
#   --images:  Удаляет локально собранные образы для сервисов проекта и повисшие образы.
#   --cache:   Очищает кэш сборки Docker.
#   --all:     Эквивалентно --volumes --images --cache (для текущего проекта).
#   --everything: (ОПАСНО!) Удаляет ВСЕ контейнеры, образы, тома, сети и кэш Docker 
#                 на системе. Использовать только в полностью изолированных окружениях.
#
# По умолчанию (без опций): останавливает и удаляет контейнеры проекта и его сеть.

# --- Начало скрипта ---
echo "============================================="
echo "==== Docker Script for Semantica Backend ===="
echo "============================================="
echo ""

# Определяем флаги по умолчанию
LIST_DOCKER_STATE=false
REMOVE_PROJECT_VOLUMES=false
REMOVE_PROJECT_IMAGES=false
PRUNE_BUILD_CACHE=false
NUKE_DOCKER_SYSTEM=false

# Парсинг аргументов командной строки
if [ "$#" -eq 0 ] && [ ! -t 0 ] ; then # Если нет аргументов и не интерактивный терминал (например, pipe)
    echo "INFO: No options provided. Performing default cleanup (down --remove-orphans)."
    # Поведение по умолчанию остается docker compose down --remove-orphans
elif [ "$#" -gt 0 ]; then
    while [[ "$#" -gt 0 ]]; do
        case $1 in
            --list) LIST_DOCKER_STATE=true; echo "INFO: Option --list selected. Displaying Docker state." ;;
            --volumes) REMOVE_PROJECT_VOLUMES=true; echo "INFO: Project volumes will be removed." ;;
            --images) REMOVE_PROJECT_IMAGES=true; echo "INFO: Project images will be removed." ;;
            --cache) PRUNE_BUILD_CACHE=true; echo "INFO: Docker builder cache will be pruned." ;;
            --all)
                REMOVE_PROJECT_VOLUMES=true
                REMOVE_PROJECT_IMAGES=true
                PRUNE_BUILD_CACHE=true
                echo "INFO: Option --all selected: project volumes, images, and builder cache will be targeted for removal."
                ;;
            --everything)
                NUKE_DOCKER_SYSTEM=true
                ;;
            *) echo "ERROR: Unknown parameter passed: $1"; exit 1 ;;
        esac
        shift
    done
else # Если нет аргументов, но запущен интерактивно, можно показать usage или выполнить --list
     echo "INFO: No specific cleanup options provided. Default action will be 'docker compose down --remove-orphans'."
     echo "INFO: Use --list to see current Docker state, or other options for cleanup."
fi


# --- Вывод состояния Docker (если указано --list) ---
if [ "$LIST_DOCKER_STATE" = true ]; then
    echo ""
    echo "--------------------------------------------------"
    echo "-------------- Current Docker State --------------"
    echo "--------------------------------------------------"
    
    echo ""
    echo "=== 1. Running Containers ==="
    docker ps
    
    echo ""
    echo "=== 2. All Containers (including stopped) ==="
    docker ps -a
    
    echo ""
    echo "=== 3. Docker Volumes ==="
    docker volume ls
    
    echo ""
    echo "=== 4. Docker Networks ==="
    docker network ls
    
    echo ""
    echo "=== 5. Docker Images (top 20) ==="
    docker images | head -n 20
    
    echo ""
    echo "=== 6. Docker Disk Usage (including Build Cache) ==="
    docker system df -v
    
    echo ""
    echo "-------------------------------------------------"
    echo "--------- Docker State Listing Finished ---------"
    echo "-------------------------------------------------"
    # Если была только опция --list, то выходим, не выполняя очистку
    if [ "$REMOVE_PROJECT_VOLUMES" = false ] && \
       [ "$REMOVE_PROJECT_IMAGES" = false ] && \
       [ "$PRUNE_BUILD_CACHE" = false ] && \
       [ "$NUKE_DOCKER_SYSTEM" = false ]; then
        exit 0
    fi
    echo "" # Дополнительный отступ перед началом очистки, если она будет
fi


# --- Полная очистка Docker (если указано --everything) ---
if [ "$NUKE_DOCKER_SYSTEM" = true ]; then
    echo ""
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    echo "!!!                                                               !!!"
    echo "!!!                        W A R N I N G                          !!!"
    echo "!!!                                                               !!!"
    echo "!!!  The --everything option will remove ALL Docker containers,   !!!"
    echo "!!!  images, volumes, networks and build cache on this system.    !!!"
    echo "!!!                                                               !!!"
    echo "!!!  This is NOT a reversible operation!                          !!!"
    echo "!!!                                                               !!!"
    echo "!!!  MAKE SURE YOU RUN THIS IN A COMPLETELY ISOLATED ENVIRONMENT  !!!"
    echo "!!!  WITH NO OTHER IMPORTANT DOCKER RESOURCES.                    !!!"
    echo "!!!                                                               !!!"
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    read -p "Вы абсолютно уверены, что хотите продолжить полную очистку Docker? (введите 'yes' для подтверждения): " confirmation
    if [[ "$confirmation" != "yes" ]]; then
        echo "Полная очистка Docker отменена пользователем."
        exit 0
    fi

    echo "INFO: Proceeding with FULL Docker system cleanup..."

    echo "INFO: Stopping all running containers..."
    docker stop $(docker ps -aq) 2>/dev/null || echo "INFO: No running containers to stop or an error occurred."

    echo "INFO: Removing all containers..."
    docker rm -f $(docker ps -aq) 2>/dev/null || echo "INFO: No containers to remove or an error occurred."
    
    echo "INFO: Removing all Docker images..."
    docker rmi -f $(docker images -aq) 2>/dev/null || echo "INFO: No images to remove or an error occurred."

    echo "INFO: Removing all Docker volumes..."
    docker volume rm $(docker volume ls -q) 2>/dev/null || echo "INFO: No volumes to remove or an error occurred."
    
    echo "INFO: Removing all Docker networks (except predefined ones)..."
    docker network rm $(docker network ls -q | grep -v -E '^(bridge|host|none)$') 2>/dev/null || echo "INFO: No custom networks to remove or an error occurred."
    
    echo "INFO: Pruning Docker builder cache..."
    docker builder prune -af
    
    echo "INFO: Performing a general Docker system prune (includes dangling images, unused networks, stopped containers)..."
    docker system prune -af --volumes # --volumes здесь удалит и неиспользуемые тома

    echo "SUCCESS: FULL Docker system cleanup finished."
    exit 0
fi

# --- Стандартная очистка для текущего проекта (выполняется, если не было --everything и есть другие флаги или нет флагов) ---
echo "INFO: Starting project-specific cleanup..."

echo "INFO: Stopping and removing project containers and associated network (if any)..."
docker compose down --remove-orphans
if [ $? -ne 0 ]; then
    echo "WARNING: 'docker compose down --remove-orphans' encountered an issue. Continuing cleanup..."
fi

if [ "$REMOVE_PROJECT_VOLUMES" = true ]; then
    echo "INFO: Removing Docker volumes associated with the project (using 'docker compose down -v')..."
    docker compose down -v --remove-orphans
    if [ $? -ne 0 ]; then
        echo "WARNING: 'docker compose down -v' encountered an issue. Some volumes might remain."
    else
        echo "INFO: Project-specific volumes (defined in docker-compose.yaml) removed."
    fi
else
    echo "INFO: Skipping project-specific volume removal (use --volumes or --all to remove them)."
fi

if [ "$REMOVE_PROJECT_IMAGES" = true ]; then
    echo "INFO: Attempting to remove locally built images for this project..."
    COMPOSE_IMAGE_IDS=$(docker compose images -q 2>/dev/null)
    if [ -n "$COMPOSE_IMAGE_IDS" ]; then
        echo "INFO: Found images used by this compose project (via 'docker compose images -q'):"
        docker images --filter "id=$(echo $COMPOSE_IMAGE_IDS | sed 's/ / --filter id=/g')"
        echo "INFO: Removing these images..."
        docker rmi $COMPOSE_IMAGE_IDS 2>/dev/null || echo "WARNING: Could not remove all images identified by 'docker compose images -q'."
    else
        echo "INFO: No specific images found for this compose project via 'docker compose images -q'. Trying by name pattern."
        PROJECT_NAME=$(basename "$PWD" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]//g')
        if [ -n "$PROJECT_NAME" ]; then
            PROJECT_IMAGE_IDS_BY_PATTERN=$(docker images --filter "reference=${PROJECT_NAME}-*" -q)
            if [ -n "$PROJECT_IMAGE_IDS_BY_PATTERN" ]; then
                echo "INFO: Found images matching pattern '${PROJECT_NAME}-*':"
                docker images --filter "reference=${PROJECT_NAME}-*"
                echo "INFO: Removing these images..."
                docker rmi $PROJECT_IMAGE_IDS_BY_PATTERN 2>/dev/null || echo "WARNING: Could not remove all images matching pattern '${PROJECT_NAME}-*'."
            else
                echo "INFO: No images found matching pattern '${PROJECT_NAME}-*'."
            fi
        else
            echo "WARNING: Could not determine project name from PWD to remove images by pattern."
        fi
    fi
    
    echo "INFO: Pruning dangling images (images without tags)..."
    docker image prune -f
else
    echo "INFO: Skipping project-specific image removal (use --images or --all to remove them)."
fi

if [ "$PRUNE_BUILD_CACHE" = true ]; then
    echo "INFO: Pruning Docker builder cache..."
    docker builder prune -af
    if [ $? -ne 0 ]; then
        echo "WARNING: Failed to prune builder cache."
    fi
else
    echo "INFO: Skipping builder cache pruning (use --cache or --all to prune)."
fi

echo ""
echo "============================================"
echo "========= Project cleanup finished ========="
echo "============================================"