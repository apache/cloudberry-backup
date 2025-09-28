## Using the S3 Storage Plugin with gpbackup and gprestore
The S3 plugin lets you use an Amazon Simple Storage Service (Amazon S3) location to store and retrieve backups when you run gpbackup and gprestore.

To use the S3 plugin, you specify the location of the plugin and the AWS login and backup location in a configuration file. When you run gpbackup or gprestore, you specify the configuration file with the option --plugin-config.

If you perform a backup operation with the gpbackup option --plugin-config, you must also specify the --plugin-config option when you restore the backup with gprestore.

The S3 plugin supports both AWS and custom storage servers that implement the S3 interface.

## S3 Storage Plugin Configuration File Format
The configuration file specifies the absolute path to the gpbackup_s3_plugin executable, AWS connection credentials, and S3 location.

The configuration file must be a valid YAML document in the following format: 

```
executablepath: <absolute-path-to-gpbackup_s3_plugin>
options: 
  region: <aws-region>
  endpoint: <s3-endpoint>
  aws_access_key_id: <aws-user-id>
  aws_secret_access_key: <aws-user-id-key>
  bucket: <s3-bucket>
  folder: <s3-location>
  encryption: [on|off]
  http_proxy: <http-proxy>
 ```

`executablepath` is the absolute path to the plugin executable (eg: use the fully expanded path of $GPHOME/bin/gpbackup_s3_plugin).

Below are the s3 plugin options

| Option Name | Description |
| --- | --- |
| `region`      | aws region (will be ignored if `endpoint` is specified |
| `endpoint`    | endpoint to a server implementing the S3 interface |
| `aws_access_key_id`      | AWS S3 ID to access the S3 bucket location that stores backup files |
| `aws_secret_access_key`       | AWS S3 passcode for the S3 ID to access the S3 bucket location |
| `bucket` | name of the S3 bucket. The bucket must exist with the necessary permissions |
| `folder` | S3 location for backups. During a backup operation, the plugin creates the S3 location if it does not exist in the S3 bucket. |
| `encryption` | Enable or disable SSL encryption to connect to S3. Valid values are on and off. On by default |
| `http_proxy` | your http proxy url |
| `backup_max_concurrent_requests` | concurrency level for any file's backup request |
| `backup_multipart_chunksize` | maximum buffer/chunk size for multipart transfers during backup |
| `restore_max_concurrent_requests` | concurrency level for any file's restore request |
| `restore_multipart_chunksize` | maximum buffer/chunk size for multipart transfers during restore |

## Example
This is an example S3 storage plugin configuration file that is used in the next gpbackup example command. The name of the file is s3-test-config.yaml.

```
executablepath: $GPHOME/bin/gpbackup_s3_plugin
options: 
  region: us-west-2
  aws_access_key_id: test-s3-user
  aws_secret_access_key: asdf1234asdf
  bucket: gpdb-backup
  folder: test/backup3
```

This gpbackup example backs up the database demo using the S3 storage plugin. The absolute path to the S3 storage plugin configuration file is /home/gpadmin/s3-test.

```
gpbackup --dbname demo --single-data-file --plugin-config /home/gpadmin/s3-test-config.yaml
```
The S3 storage plugin writes the backup files to this S3 location in the AWS region us-west-2.

```
gpdb-backup/test/backup3/backups/YYYYMMDD/YYYYMMDDHHMMSS/
```

## Notes
The S3 storage plugin application must be in the same location on every Greenplum Database host. The configuration file is required only on the coordinator host.

Using Amazon S3 to back up and restore data requires an Amazon AWS account with access to the Amazon S3 bucket. The Amazon S3 bucket permissions required are Upload/Delete for the S3 user ID that uploads the files and Open/Download and View for the S3 user ID that accesses the files.
