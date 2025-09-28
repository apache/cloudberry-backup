package s3plugin_test

import (
	"errors"
	"flag"
	"net/http"
	"testing"

	"github.com/apache/cloudberry-backup/plugins/s3plugin"
	"github.com/apache/cloudberry-go-libs/testhelper"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/urfave/cli"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "s3_plugin tests")
}

var _ = Describe("s3_plugin tests", func() {
	var pluginConfig *s3plugin.PluginConfig
	var opts *s3plugin.PluginOptions
	BeforeEach(func() {
		pluginConfig = &s3plugin.PluginConfig{
			ExecutablePath: "/tmp/location",
			Options: s3plugin.PluginOptions{
				AwsAccessKeyId:               "12345",
				AwsSecretAccessKey:           "6789",
				BackupMaxConcurrentRequests:  "5",
				BackupMultipartChunksize:     "7MB",
				Bucket:                       "bucket_name",
				Endpoint:                     "endpoint_name",
				Folder:                       "folder_name",
				Region:                       "region_name",
				RestoreMaxConcurrentRequests: "5",
				RestoreMultipartChunksize:    "7MB",
			},
		}
		opts = &pluginConfig.Options
	})
	Describe("GetS3Path", func() {
		It("it combines the folder directory with a path that results from removing all but the last 3 directories of the file path parameter", func() {
			folder := "s3/Dir"
			path := "/a/b/c/tmp/datadir/gpseg-1/backups/20180101/20180101082233/backup_file"
			newPath := s3plugin.GetS3Path(folder, path)
			expectedPath := "s3/Dir/backups/20180101/20180101082233/backup_file"
			Expect(newPath).To(Equal(expectedPath))
		})
	})
	Describe("ShouldEnableEncryption", func() {
		It("returns true when no encryption in config", func() {
			result := s3plugin.ShouldEnableEncryption("")
			Expect(result).To(BeTrue())
		})
		It("returns true when encryption set to 'on' in config", func() {
			result := s3plugin.ShouldEnableEncryption("on")
			Expect(result).To(BeTrue())
		})
		It("returns false when encryption set to 'off' in config", func() {
			result := s3plugin.ShouldEnableEncryption("off")
			Expect(result).To(BeFalse())
		})
		It("returns true when encryption set to anything else in config", func() {
			result := s3plugin.ShouldEnableEncryption("random_test")
			Expect(result).To(BeTrue())
		})
	})
	Describe("InitializeAndValidateConfig", func() {
		Context("Sets defaults", func() {
			It("sets region to unused when endpoint is used instead of region", func() {
				opts.Region = ""
				err := s3plugin.InitializeAndValidateConfig(pluginConfig)
				Expect(err).To(BeNil())
				Expect(opts.Region).To(Equal("unused"))
			})
			It(`sets encryption to default value "on" if none is specified`, func() {
				opts.Encryption = ""
				err := s3plugin.InitializeAndValidateConfig(pluginConfig)
				Expect(err).To(BeNil())
				Expect(opts.Encryption).To(Equal("on"))
			})
			It("sets backup upload chunk size to default if BackupMultipartChunkSize is not specified", func() {
				opts.BackupMultipartChunksize = ""
				err := s3plugin.InitializeAndValidateConfig(pluginConfig)
				Expect(err).To(BeNil())
				Expect(opts.UploadChunkSize).To(Equal(s3plugin.DefaultUploadChunkSize))
			})
			It("sets backup upload concurrency to default if BackupMaxConcurrentRequests is not specified", func() {
				opts.BackupMaxConcurrentRequests = ""
				err := s3plugin.InitializeAndValidateConfig(pluginConfig)
				Expect(err).To(BeNil())
				Expect(opts.UploadConcurrency).To(Equal(s3plugin.DefaultConcurrency))
			})
			It("sets restore download chunk size to default if RestoreMultipartChunkSize is not specified", func() {
				opts.RestoreMultipartChunksize = ""
				err := s3plugin.InitializeAndValidateConfig(pluginConfig)
				Expect(err).To(BeNil())
				Expect(opts.DownloadChunkSize).To(Equal(s3plugin.DefaultDownloadChunkSize))
			})
			It("sets restore download concurrency to default is RestoreMaxConcurrentRequests is not specified", func() {
				opts.RestoreMaxConcurrentRequests = ""
				err := s3plugin.InitializeAndValidateConfig(pluginConfig)
				Expect(err).To(BeNil())
				Expect(opts.DownloadConcurrency).To(Equal(s3plugin.DefaultConcurrency))
			})
		})
		It("succeeds when all fields in config filled", func() {
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(BeNil())
		})
		It("succeeds when all fields except endpoint filled in config", func() {
			opts.Endpoint = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(BeNil())
		})
		It("succeeds when all fields except region filled in config", func() {
			opts.Region = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(BeNil())
		})
		It("succeeds when all fields except aws_access_key_id and aws_secret_access_key in config", func() {
			opts.AwsAccessKeyId = ""
			opts.AwsSecretAccessKey = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(BeNil())
		})
		It("returns error when neither region nor endpoint in config", func() {
			opts.Region = ""
			opts.Endpoint = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error when no aws_access_key_id in config", func() {
			opts.AwsAccessKeyId = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error when no aws_secret_access_key in config", func() {
			opts.AwsSecretAccessKey = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error when no bucket in config", func() {
			opts.Bucket = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error when no folder in config", func() {
			opts.Folder = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error when the encryption value is invalid", func() {
			opts.Encryption = "invalid_value"
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error when the encryption value is invalid", func() {
			opts.Encryption = "invalid_value"
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("returns error if executable path is missing", func() {
			pluginConfig.ExecutablePath = ""
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(HaveOccurred())
		})
		It("correctly parses upload params from config", func() {
			opts.BackupMultipartChunksize = "10MB"
			opts.BackupMaxConcurrentRequests = "10"
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(BeNil())
			Expect(opts.UploadChunkSize).To(Equal(int64(10 * 1024 * 1024)))
			Expect(opts.UploadConcurrency).To(Equal(10))
		})
		It("correctly parses download params from config", func() {
			opts.RestoreMultipartChunksize = "10GB"
			opts.RestoreMaxConcurrentRequests = "10"
			err := s3plugin.InitializeAndValidateConfig(pluginConfig)
			Expect(err).To(BeNil())
			Expect(opts.DownloadChunkSize).To(Equal(int64(10 * 1024 * 1024 * 1024)))
			Expect(opts.DownloadConcurrency).To(Equal(10))
		})
	})
	Describe("Delete", func() {
		var flags *flag.FlagSet

		BeforeEach(func() {
			flags = flag.NewFlagSet("testing flagset", flag.PanicOnError)
		})
		It("returns error when timestamp is not provided", func() {
			err := flags.Parse([]string{"myconfigfilepath"})
			Expect(err).ToNot(HaveOccurred())
			context := cli.NewContext(nil, flags, nil)

			err = s3plugin.DeleteBackup(context)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("delete requires a <timestamp>"))
		})
		It("returns error when timestamp does not parse", func() {
			err := flags.Parse([]string{"myconfigfilepath", "badformat"})
			Expect(err).ToNot(HaveOccurred())
			context := cli.NewContext(nil, flags, nil)

			err = s3plugin.DeleteBackup(context)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("delete requires a <timestamp> with format YYYYMMDDHHMMSS, but received: badformat"))
		})
	})
	Describe("CustomRetryer", func() {
		DescribeTable("validate retryer on different http status codes",
			func(httpStatusCode int, testError error, expectedRetryValue bool) {
				_, _, _ = testhelper.SetupTestLogger()
				var awsErr awserr.RequestFailure
				if testError != nil {
					awsErr = awserr.NewRequestFailure(awserr.New(request.ErrCodeRequestError, testError.Error(), testError), httpStatusCode, "")
				}
				req := &request.Request{
					HTTPResponse: &http.Response{StatusCode: httpStatusCode},
					Error:        awsErr,
				}
				retryer := s3plugin.CustomRetryer{DefaultRetryer: client.DefaultRetryer{NumMaxRetries: 5}}
				retryValue := retryer.ShouldRetry(req)
				if retryValue != expectedRetryValue {
					Fail("unexpected retry behavior")
				}
			},
			Entry("status OK", 200, nil, false),
			Entry("connection reset", 104, errors.New("connection reset by peer"), true),
			Entry("NoSuchKey", 404, awserr.New(s3.ErrCodeNoSuchKey, "No Such Key", nil), true),
			Entry("NoSuchBucket", 404, awserr.New(s3.ErrCodeNoSuchBucket, "No Such Bucket", nil), false),
			Entry("NotFound", 404, awserr.New("NotFound", "Not Found", nil), false),
		)
	})
})
