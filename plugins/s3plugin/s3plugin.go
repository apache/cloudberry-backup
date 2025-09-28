package s3plugin

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/apache/cloudberry-go-libs/operating"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/inhies/go-bytesize"
	"github.com/olekukonko/tablewriter"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var Version string

const apiVersion = "0.5.0"
const Mebibyte = 1024 * 1024
const DefaultConcurrency = 6
const DefaultUploadChunkSize = int64(Mebibyte) * 500   // default 500MB
const DefaultDownloadChunkSize = int64(Mebibyte) * 500 // default 500MB

type Scope string

const (
	Master      Scope = "master"
	Coordinator Scope = "coordinator"
	SegmentHost Scope = "segment_host"
	Segment     Scope = "segment"
)

type PluginConfig struct {
	ExecutablePath string        `yaml:"executablepath"`
	Options        PluginOptions `yaml:"options"`
}

type PluginOptions struct {
	AwsAccessKeyId               string `yaml:"aws_access_key_id"`
	AwsSecretAccessKey           string `yaml:"aws_secret_access_key"`
	BackupMaxConcurrentRequests  string `yaml:"backup_max_concurrent_requests"`
	BackupMultipartChunksize     string `yaml:"backup_multipart_chunksize"`
	Bucket                       string `yaml:"bucket"`
	Encryption                   string `yaml:"encryption"`
	Endpoint                     string `yaml:"endpoint"`
	Folder                       string `yaml:"folder"`
	HttpProxy                    string `yaml:"http_proxy"`
	Region                       string `yaml:"region"`
	RemoveDuplicateBucket        string `yaml:"remove_duplicate_bucket"`
	RestoreMaxConcurrentRequests string `yaml:"restore_max_concurrent_requests"`
	RestoreMultipartChunksize    string `yaml:"restore_multipart_chunksize"`
	PgPort                       string `yaml:"pgport"`
	BackupPluginVersion          string `yaml:"backup_plugin_version"`

	UploadChunkSize     int64
	UploadConcurrency   int
	DownloadChunkSize   int64
	DownloadConcurrency int
}

func CleanupPlugin(c *cli.Context) error {
	return nil
}

func GetAPIVersion(c *cli.Context) {
	fmt.Println(apiVersion)
}

/*
 * Helper Functions
 */

func readAndValidatePluginConfig(configFile string) (*PluginConfig, error) {
	config := &PluginConfig{}
	contents, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	if err = yaml.UnmarshalStrict(contents, config); err != nil {
		return nil, fmt.Errorf("Yaml failures encountered reading config file %s. Error: %s", configFile, err.Error())
	}
	if err = InitializeAndValidateConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

func InitializeAndValidateConfig(config *PluginConfig) error {
	var err error
	var errTxt string
	opt := &config.Options

	// Initialize defaults
	if opt.Region == "" {
		opt.Region = "unused"
	}
	if opt.Encryption == "" {
		opt.Encryption = "on"
	}
	if opt.RemoveDuplicateBucket == "" {
		opt.RemoveDuplicateBucket = "false"
	}
	opt.UploadChunkSize = DefaultUploadChunkSize
	opt.UploadConcurrency = DefaultConcurrency
	opt.DownloadChunkSize = DefaultDownloadChunkSize
	opt.DownloadConcurrency = DefaultConcurrency

	// Validate configurations and overwrite defaults
	if config.ExecutablePath == "" {
		errTxt += fmt.Sprintf("executable_path must exist and cannot be empty in plugin configuration file\n")
	}
	if opt.Bucket == "" {
		errTxt += fmt.Sprintf("bucket must exist and cannot be empty in plugin configuration file\n")
	}
	if opt.Folder == "" {
		errTxt += fmt.Sprintf("folder must exist and cannot be empty in plugin configuration file\n")
	}
	if opt.AwsAccessKeyId == "" {
		if opt.AwsSecretAccessKey != "" {
			errTxt += fmt.Sprintf("aws_access_key_id must exist in plugin configuration file if aws_secret_access_key does\n")
		}
	} else if opt.AwsSecretAccessKey == "" {
		errTxt += fmt.Sprintf("aws_secret_access_key must exist in plugin configuration file if aws_access_key_id does\n")
	}
	if opt.Region == "unused" && opt.Endpoint == "" {
		errTxt += fmt.Sprintf("region or endpoint must exist in plugin configuration file\n")
	}
	if opt.Encryption != "on" && opt.Encryption != "off" {
		errTxt += fmt.Sprintf("Invalid encryption configuration. Valid choices are on or off.\n")
	}
	if opt.RemoveDuplicateBucket != "true" && opt.RemoveDuplicateBucket != "false" {
		errTxt += fmt.Sprintf("Invalid value for remove_duplicate_bucket. Valid choices are true or false.\n")
	}
	if opt.BackupMultipartChunksize != "" {
		chunkSize, err := bytesize.Parse(opt.BackupMultipartChunksize)
		if err != nil {
			errTxt += fmt.Sprintf("Invalid backup_multipart_chunksize. Err: %s\n", err)
		}
		// Chunk size is being converted from uint64 to int64. This is safe as
		// long as chunksize smaller than math.MaxInt64 bytes (~9223 Petabytes)
		opt.UploadChunkSize = int64(chunkSize)
	}
	if opt.BackupMaxConcurrentRequests != "" {
		opt.UploadConcurrency, err = strconv.Atoi(opt.BackupMaxConcurrentRequests)
		if err != nil {
			errTxt += fmt.Sprintf("Invalid backup_max_concurrent_requests. Err: %s\n", err)
		}
	}
	if opt.RestoreMultipartChunksize != "" {
		chunkSize, err := bytesize.Parse(opt.RestoreMultipartChunksize)
		if err != nil {
			errTxt += fmt.Sprintf("Invalid restore_multipart_chunksize. Err: %s\n", err)
		}
		// Chunk size is being converted from uint64 to int64. This is safe as
		// long as chunksize smaller than math.MaxInt64 bytes (~9223 Petabytes)
		opt.DownloadChunkSize = int64(chunkSize)
	}
	if opt.RestoreMaxConcurrentRequests != "" {
		opt.DownloadConcurrency, err = strconv.Atoi(opt.RestoreMaxConcurrentRequests)
		if err != nil {
			errTxt += fmt.Sprintf("Invalid restore_max_concurrent_requests. Err: %s\n", err)
		}
	}

	if errTxt != "" {
		return errors.New(errTxt)
	}
	return nil
}

// CustomRetryer wraps the SDK's built in DefaultRetryer
type CustomRetryer struct {
	client.DefaultRetryer
}

// ShouldRetry overrides the SDK's built in DefaultRetryer
func (r CustomRetryer) ShouldRetry(req *request.Request) bool {
	if r.NumMaxRetries == 0 {
		return false
	}

	willRetry := false
	if req.Error != nil && strings.Contains(req.Error.Error(), "connection reset by peer") {
		willRetry = true
	} else if req.HTTPResponse.StatusCode == 404 && strings.Contains(req.Error.Error(), "NoSuchKey") {
		// 404 NoSuchKey error is possible due to AWS's eventual consistency
		// when attempting to inspect or get a file too quickly after it was
		// uploaded. The s3 plugin does exactly this to determine the amount of
		// bytes uploaded. For this reason we retry 404 errors.
		willRetry = true
	} else {
		willRetry = r.DefaultRetryer.ShouldRetry(req)
	}

	if willRetry {
		// While its possible to let the AWS client log for us, it doesn't seem
		// possible to set it up to only log errors. To prevent our log from
		// filling up with debug logs of successful https requests and
		// response, we'll only log when retries are attempted.
		if req.Error != nil {
			gplog.Debug("Https request attempt %d failed. Next attempt in %v. %s\n", req.RetryCount, r.RetryRules(req), req.Error.Error())
		} else {
			gplog.Debug("Https request attempt %d failed. Next attempt in %v.\n", req.RetryCount, r.RetryRules(req))
		}
		return true
	}

	return false
}

func readConfigAndStartSession(c *cli.Context) (*PluginConfig, *session.Session, error) {
	configPath := c.Args().Get(0)
	config, err := readAndValidatePluginConfig(configPath)
	if err != nil {
		return nil, nil, err
	}

	disableSSL := !ShouldEnableEncryption(config.Options.Encryption)

	awsConfig := request.WithRetryer(aws.NewConfig(), CustomRetryer{DefaultRetryer: client.DefaultRetryer{NumMaxRetries: 10}}).
		WithRegion(config.Options.Region).
		WithEndpoint(config.Options.Endpoint).
		WithS3ForcePathStyle(true).
		WithDisableSSL(disableSSL).
		WithUseDualStack(true)

	// Will use default credential chain if none provided
	if config.Options.AwsAccessKeyId != "" {
		awsConfig = awsConfig.WithCredentials(
			credentials.NewStaticCredentials(
				config.Options.AwsAccessKeyId,
				config.Options.AwsSecretAccessKey, ""))
	}

	if config.Options.HttpProxy != "" {
		httpclient := &http.Client{
			Transport: &http.Transport{
				Proxy: func(*http.Request) (*url.URL, error) {
					return url.Parse(config.Options.HttpProxy)
				},
			},
		}
		awsConfig.WithHTTPClient(httpclient)
	}

	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, nil, err
	}

	if config.Options.RemoveDuplicateBucket == "true" {
		sess.Handlers.Build.PushFront(removeBucketFromPath)
	}
	return config, sess, nil
}

func ShouldEnableEncryption(encryption string) bool {
	isOff := strings.EqualFold(encryption, "off")
	return !isOff
}

func isDirectoryGetSize(path string) (bool, int64) {
	fd, err := os.Stat(path)
	if err != nil {
		gplog.FatalOnError(err)
	}
	switch mode := fd.Mode(); {
	case mode.IsDir():
		return true, 0
	case mode.IsRegular():
		return false, fd.Size()
	}
	gplog.FatalOnError(errors.New(fmt.Sprintf("INVALID file %s", path)))
	return false, 0
}

func getFileSize(S3 s3iface.S3API, bucket string, fileKey string) (int64, error) {
	req, resp := S3.HeadObjectRequest(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileKey),
	})
	err := req.Send()

	if err != nil {
		return 0, err
	}
	return *resp.ContentLength, nil
}

func GetS3Path(folder string, path string) string {
	/*
			a typical path for an already-backed-up file will be stored in a
			parent directory of a segment, and beneath that, under a datestamp/timestamp/
		    hierarchy. We assume the incoming path is a long absolute one.
			For example from the test bench:
			  testdir_for_del="/tmp/testseg/backups/$current_date_for_del/$time_second_for_del"
			  testfile_for_del="$testdir_for_del/testfile_$time_second_for_del.txt"

			Therefore, the incoming path is relevant to S3 in only the last four segments,
			which indicate the file and its 2 date/timestamp parents, and the grandparent "backups"
	*/
	pathArray := strings.Split(path, "/")
	lastFour := strings.Join(pathArray[(len(pathArray)-4):], "/")
	return fmt.Sprintf("%s/%s", folder, lastFour)
}

func DeleteBackup(c *cli.Context) error {
	timestamp := c.Args().Get(1)
	if timestamp == "" {
		return errors.New("delete requires a <timestamp>")
	}

	if !IsValidTimestamp(timestamp) {
		msg := fmt.Sprintf("delete requires a <timestamp> with format "+
			"YYYYMMDDHHMMSS, but received: %s", timestamp)
		return fmt.Errorf(msg)
	}

	date := timestamp[0:8]
	// note that "backups" is a directory is a fact of how we save, choosing
	// to use the 3 parent directories of the source file. That becomes:
	// <s3folder>/backups/<date>/<timestamp>
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	deletePath := filepath.Join(config.Options.Folder, "backups", date, timestamp)
	bucket := config.Options.Bucket
	gplog.Debug("Delete location = s3://%s/%s", bucket, deletePath)

	service := s3.New(sess)
	iter := s3manager.NewDeleteListIterator(service, &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(deletePath),
	})

	batchClient := s3manager.NewBatchDeleteWithClient(service)
	return batchClient.Delete(aws.BackgroundContext(), iter)
}

func ListDirectory(c *cli.Context) error {
	var err error
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	bucket := config.Options.Bucket

	var listPath string
	if len(c.Args()) == 2 {
		listPath = c.Args().Get(1)
	} else {
		listPath = config.Options.Folder
	}

	client := s3.New(sess)
	params := &s3.ListObjectsV2Input{Bucket: &bucket, Prefix: &listPath}
	bucketObjectsList, _ := client.ListObjectsV2(params)
	fileSizes := make([][]string, 0)

	gplog.Verbose("Retrieving file information from directory %s in S3", listPath)
	for _, key := range bucketObjectsList.Contents {
		if strings.HasSuffix(*key.Key, "/") {
			// Got a directory
			continue
		}

		downloader := s3manager.NewDownloader(sess, func(u *s3manager.Downloader) {
			u.PartSize = config.Options.DownloadChunkSize
		})

		totalBytes, err := getFileSize(downloader.S3, bucket, *key.Key)
		if err != nil {
			return err
		}

		fileSizes = append(fileSizes, []string{*key.Key, fmt.Sprint(totalBytes)})
	}

	// Render the data as a table
	table := tablewriter.NewWriter(operating.System.Stdout)
	columns := []string{"NAME", "SIZE(bytes)"}
	table.SetHeader(columns)

	colors := make([]tablewriter.Colors, len(columns))
	for i := range colors {
		colors[i] = tablewriter.Colors{tablewriter.Bold}
	}

	table.SetHeaderColor(colors...)
	table.SetCenterSeparator(" ")
	table.SetColumnSeparator(" ")
	table.SetRowSeparator(" ")
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(true)
	table.SetAutoFormatHeaders(false)
	table.SetBorders(tablewriter.Border{Left: true, Right: true, Bottom: false, Top: false})
	table.AppendBulk(fileSizes)
	table.Render()

	return err
}

func DeleteDirectory(c *cli.Context) error {
	config, sess, err := readConfigAndStartSession(c)
	if err != nil {
		return err
	}
	deletePath := c.Args().Get(1)
	bucket := config.Options.Bucket
	gplog.Verbose("Deleting directory s3://%s/%s", bucket, deletePath)
	service := s3.New(sess)
	iter := s3manager.NewDeleteListIterator(service, &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(deletePath),
	})
	batchClient := s3manager.NewBatchDeleteWithClient(service)
	return batchClient.Delete(aws.BackgroundContext(), iter)
}

func IsValidTimestamp(timestamp string) bool {
	timestampFormat := regexp.MustCompile(`^([0-9]{14})$`)
	return timestampFormat.MatchString(timestamp)
}

// Some AWS SDK automatically prepends "/BucketName/" to any request's path, which breaks placement
// of all objects when doing backups or restores with an Endpoint URL that already directs requests
// to the correct bucket. To circumvent this, we manually remove the initial Bucket reference from
// the path in this case. NOTE: this does not happen in if an IP address is used directly, so we
// attempt to parse IP addresses and do not invoke this removal if found.
func removeBucketFromPath(req *request.Request) {
	req.Operation.HTTPPath = strings.Replace(req.Operation.HTTPPath, "/{Bucket}", "", -1)
	if !strings.HasPrefix(req.Operation.HTTPPath, "/") {
		req.Operation.HTTPPath = "/" + req.Operation.HTTPPath
	}
	req.HTTPRequest.URL.Path = strings.Replace(req.HTTPRequest.URL.Path, "/{Bucket}", "", -1)
	if !strings.HasPrefix(req.HTTPRequest.URL.Path, "/") {
		req.HTTPRequest.URL.Path = "/" + req.HTTPRequest.URL.Path
	}
}
