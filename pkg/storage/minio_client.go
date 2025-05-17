// File: pkg/storage/minio_client.go
package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioClient представляет собой клиент для взаимодействия с Minio.
type MinioClient struct {
	client     *minio.Client
	bucketName string
	logger     *slog.Logger
}

// MinioConfig содержит конфигурацию для подключения к Minio.
type MinioConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
}

// NewMinioClient создает и инициализирует нового клиента Minio.
// Он также проверяет существование бакета и создает его, если он отсутствует.
func NewMinioClient(ctx context.Context, cfg MinioConfig, logger *slog.Logger) (*MinioClient, error) {
	log := logger.With("component", "minio_client")
	log.Info("Initializing Minio client...",
		"endpoint", cfg.Endpoint,
		"bucket", cfg.BucketName,
		"ssl_enabled", cfg.UseSSL)

	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		log.Error("Failed to initialize Minio client", "error", err)
		return nil, fmt.Errorf("minio.New: %w", err)
	}

	// Проверяем доступность Minio (опционально, но полезно)
	// Простой способ - попытаться получить список бакетов или информацию о бакете.
	// Для этого этапа достаточно просто инициализировать. Проверка бакета ниже.

	// Проверяем, существует ли бакет, и создаем его, если нет.
	// Таймаут для операций с бакетом
	bucketCtx, bucketCancel := context.WithTimeout(ctx, 30*time.Second)
	defer bucketCancel()

	exists, err := minioClient.BucketExists(bucketCtx, cfg.BucketName)
	if err != nil {
		log.Error("Failed to check if bucket exists", "bucket", cfg.BucketName, "error", err)
		return nil, fmt.Errorf("minioClient.BucketExists '%s': %w", cfg.BucketName, err)
	}

	if !exists {
		log.Info("Bucket does not exist, attempting to create it...", "bucket", cfg.BucketName)
		err = minioClient.MakeBucket(bucketCtx, cfg.BucketName, minio.MakeBucketOptions{}) // Используйте регион, если нужно: Region: "us-east-1"
		if err != nil {
			log.Error("Failed to create bucket", "bucket", cfg.BucketName, "error", err)
			return nil, fmt.Errorf("minioClient.MakeBucket '%s': %w", cfg.BucketName, err)
		}
		log.Info("Bucket created successfully", "bucket", cfg.BucketName)
	} else {
		log.Info("Bucket already exists", "bucket", cfg.BucketName)
	}

	return &MinioClient{
		client:     minioClient,
		bucketName: cfg.BucketName,
		logger:     log,
	}, nil
}

// UploadObject загружает объект в Minio.
// objectName: полный путь к объекту внутри бакета (например, "raw/task123/data.html").
// reader: источник данных.
// size: размер данных в байтах. Если -1, размер будет определен из reader (может быть менее эффективно для некоторых reader'ов).
// contentType: MIME-тип контента (например, "text/html", "application/pdf").
func (mc *MinioClient) UploadObject(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (minio.UploadInfo, error) {
	mc.logger.Debug("Uploading object to Minio...",
		"bucket", mc.bucketName,
		"object", objectName,
		"size", size,
		"contentType", contentType)

	uploadInfo, err := mc.client.PutObject(ctx, mc.bucketName, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
		// Можно добавить другие опции, например, UserMetadata
	})
	if err != nil {
		mc.logger.Error("Failed to upload object", "bucket", mc.bucketName, "object", objectName, "error", err)
		return minio.UploadInfo{}, fmt.Errorf("mc.client.PutObject (bucket: %s, object: %s): %w", mc.bucketName, objectName, err)
	}

	mc.logger.Info("Object uploaded successfully",
		"bucket", mc.bucketName,
		"object", objectName,
		"etag", uploadInfo.ETag,
		"version_id", uploadInfo.VersionID,
		"location", uploadInfo.Location)
	return uploadInfo, nil
}

// GetObject получает объект из Minio.
// objectName: полный путь к объекту внутри бакета.
// Возвращает *minio.Object, который является io.ReadCloser. Не забудьте его закрыть.
func (mc *MinioClient) GetObject(ctx context.Context, objectName string) (*minio.Object, error) {
	mc.logger.Debug("Getting object from Minio...", "bucket", mc.bucketName, "object", objectName)

	object, err := mc.client.GetObject(ctx, mc.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		mc.logger.Error("Failed to get object", "bucket", mc.bucketName, "object", objectName, "error", err)
		return nil, fmt.Errorf("mc.client.GetObject (bucket: %s, object: %s): %w", mc.bucketName, objectName, err)
	}

	// Проверка, что объект действительно существует (GetObject может не вернуть ошибку сразу, если объекта нет)
	// stat, err := object.Stat()
	// if err != nil {
	// 	object.Close() // Важно закрыть, даже если Stat вернул ошибку
	// 	mc.logger.Error("Failed to stat object after GetObject (object might not exist or access issue)", "bucket", mc.bucketName, "object", objectName, "error", err)
	// 	return nil, fmt.Errorf("object.Stat (bucket: %s, object: %s): %w", mc.bucketName, objectName, err)
	// }
	// mc.logger.Info("Object retrieved successfully", "bucket", mc.bucketName, "object", objectName, "size", stat.Size, "etag", stat.ETag)

	return object, nil
}

// PresignedPutURL генерирует presigned URL для загрузки объекта.
func (mc *MinioClient) PresignedPutURL(ctx context.Context, objectName string, expiry time.Duration) (*url.URL, error) {
	mc.logger.Debug("Generating presigned PUT URL...", "bucket", mc.bucketName, "object", objectName, "expiry", expiry)
	presignedURL, err := mc.client.PresignedPutObject(ctx, mc.bucketName, objectName, expiry)
	if err != nil {
		mc.logger.Error("Failed to generate presigned PUT URL", "bucket", mc.bucketName, "object", objectName, "error", err)
		return nil, fmt.Errorf("mc.client.PresignedPutObject: %w", err)
	}
	mc.logger.Info("Presigned PUT URL generated", "bucket", mc.bucketName, "object", objectName)
	return presignedURL, nil
}

// PresignedGetURL генерирует presigned URL для скачивания объекта.
func (mc *MinioClient) PresignedGetURL(ctx context.Context, objectName string, expiry time.Duration) (*url.URL, error) {
	mc.logger.Debug("Generating presigned GET URL...", "bucket", mc.bucketName, "object", objectName, "expiry", expiry)
	// Можно добавить reqParam для установки Content-Disposition и т.д.
	// reqParams := make(url.Values)
	// reqParams.Set("response-content-disposition", "attachment; filename=\"your-filename.ext\"")
	presignedURL, err := mc.client.PresignedGetObject(ctx, mc.bucketName, objectName, expiry, nil) //nil -> reqParams
	if err != nil {
		mc.logger.Error("Failed to generate presigned GET URL", "bucket", mc.bucketName, "object", objectName, "error", err)
		return nil, fmt.Errorf("mc.client.PresignedGetObject: %w", err)
	}
	mc.logger.Info("Presigned GET URL generated", "bucket", mc.bucketName, "object", objectName)
	return presignedURL, nil
}

// GetBucketName возвращает имя бакета, с которым работает клиент.
func (mc *MinioClient) GetBucketName() string {
	return mc.bucketName
}
