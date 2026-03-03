package s3plugin

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func SetupPluginForBackup(c *cli.Context) error {
	scope := (Scope)(c.Args().Get(2))
	if scope != Master && scope != Coordinator && scope != SegmentHost {
		return nil
	}
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	localBackupDir := c.Args().Get(1)
	_, timestamp := filepath.Split(localBackupDir)
	testFileName := fmt.Sprintf("gpbackup_%s_report", timestamp)
	testFilePath := fmt.Sprintf("%s/%s", localBackupDir, testFileName)
	fileKey := GetS3Path(config.Options.Folder, testFilePath)
	file, err := os.Create("/tmp/" + testFileName) // dummy empty reader for probe
	defer file.Close()
	if err != nil {
		return err
	}
	_, _, err = uploadFile(sess, config, fileKey, file)
	return err
}

func BackupFile(c *cli.Context) error {
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	fileName := c.Args().Get(1)
	fileKey := GetS3Path(config.Options.Folder, fileName)
	file, err := os.Open(fileName)
	defer file.Close()
	if err != nil {
		return err
	}
	bytes, elapsed, err := uploadFile(sess, config, fileKey, file)
	if err != nil {
		return err
	}

	gplog.Info("Uploaded %d bytes for %s in %v", bytes, filepath.Base(fileKey),
		elapsed.Round(time.Millisecond))
	return nil
}

func BackupDirectory(c *cli.Context) error {
	start := time.Now()
	totalBytes := int64(0)
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	dirName := c.Args().Get(1)
	gplog.Verbose("Restore Directory '%s' from S3", dirName)
	gplog.Verbose("S3 Location = s3://%s/%s", config.Options.Bucket, dirName)
	gplog.Info("dirKey = %s\n", dirName)

	// Populate a list of files to be backed up
	fileList := make([]string, 0)
	_ = filepath.Walk(dirName, func(path string, f os.FileInfo, err error) error {
		isDir, _ := isDirectoryGetSize(path)
		if !isDir {
			fileList = append(fileList, path)
		}
		return nil
	})

	// Process the files sequentially
	for _, fileName := range fileList {
		file, err := os.Open(fileName)
		if err != nil {
			return err
		}
		bytes, elapsed, err := uploadFile(sess, config, fileName, file)
		_ = file.Close()
		if err != nil {
			return err
		}

		totalBytes += bytes
		gplog.Debug("Uploaded %d bytes for %s in %v", bytes,
			filepath.Base(fileName), elapsed.Round(time.Millisecond))
	}

	gplog.Info("Uploaded %d files (%d bytes) in %v\n", len(fileList),
		totalBytes, time.Since(start).Round(time.Millisecond))
	return nil
}

func BackupDirectoryParallel(c *cli.Context) error {
	start := time.Now()
	totalBytes := int64(0)
	parallel := 5
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	dirName := c.Args().Get(1)
	if len(c.Args()) == 3 {
		parallel, _ = strconv.Atoi(c.Args().Get(2))
	}
	gplog.Verbose("Backup Directory '%s' to S3", dirName)
	gplog.Verbose("S3 Location = s3://%s/%s", config.Options.Bucket, dirName)
	gplog.Info("dirKey = %s\n", dirName)

	// Populate a list of files to be backed up
	fileList := make([]string, 0)
	_ = filepath.Walk(dirName, func(path string, f os.FileInfo, err error) error {
		isDir, _ := isDirectoryGetSize(path)
		if !isDir {
			fileList = append(fileList, path)
		}
		return nil
	})

	var wg sync.WaitGroup
	var finalErr error
	// Create jobs using a channel
	fileChannel := make(chan string, len(fileList))
	for _, fileKey := range fileList {
		wg.Add(1)
		fileChannel <- fileKey
	}
	close(fileChannel)
	// Process the files in parallel
	for i := 0; i < parallel; i++ {
		go func(jobs chan string) {
			for fileKey := range jobs {
				file, err := os.Open(fileKey)
				if err != nil {
					finalErr = err
					return
				}
				bytes, elapsed, err := uploadFile(sess, config, fileKey, file)
				if err == nil {
					totalBytes += bytes
					msg := fmt.Sprintf("Uploaded %d bytes for %s in %v", bytes,
						filepath.Base(fileKey), elapsed.Round(time.Millisecond))
					gplog.Verbose(msg)
					fmt.Println(msg)
				} else {
					finalErr = err
					gplog.FatalOnError(err)
				}
				_ = file.Close()
				wg.Done()
			}
		}(fileChannel)
	}
	// Wait for jobs to be done
	wg.Wait()

	gplog.Info("Uploaded %d files (%d bytes) in %v\n",
		len(fileList), totalBytes, time.Since(start).Round(time.Millisecond))
	return finalErr
}

func BackupData(c *cli.Context) error {
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	dataFile := c.Args().Get(1)
	fileKey := GetS3Path(config.Options.Folder, dataFile)

	bytes, elapsed, err := uploadFile(sess, config, fileKey, os.Stdin)
	if err != nil {
		return err
	}

	gplog.Debug("Uploaded %d bytes for file %s in %v", bytes,
		filepath.Base(fileKey), elapsed.Round(time.Millisecond))
	return nil
}

func uploadFile(sess *session.Session, config *PluginConfig, fileKey string,
	file *os.File) (int64, time.Duration, error) {

	start := time.Now()
	bucket := config.Options.Bucket
	uploadChunkSize := config.Options.UploadChunkSize
	uploadConcurrency := config.Options.UploadConcurrency

	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = uploadChunkSize
		u.Concurrency = uploadConcurrency
	})
	gplog.Debug("Uploading file %s with chunksize %d and concurrency %d",
		filepath.Base(fileKey), uploader.PartSize, uploader.Concurrency)
	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileKey),
		// This will cause memory issues if
		// segment_per_host*uploadChunkSize*uploadConcurreny is larger than
		// the amount of ram a system has.
		Body: bufio.NewReaderSize(file, int(uploadChunkSize)*uploadConcurrency),
	})
	if err != nil {
		return 0, -1, errors.Wrap(err, fmt.Sprintf("Error while uploading %s", fileKey))
	}
	bytes, err := getFileSize(uploader.S3, bucket, fileKey)
	return bytes, time.Since(start), err
}
