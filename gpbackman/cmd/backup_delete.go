package cmd

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/apache/cloudberry-go-libs/operating"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-backup/history"
	"github.com/apache/cloudberry-backup/utils"
)

// Flags for the gpbackman backup-delete command (backupDeleteCmd)
var (
	backupDeleteTimestamp         []string
	backupDeletePluginConfigFile  string
	backupDeleteBackupDir         string
	backupDeleteCascade           bool
	backupDeleteForce             bool
	backupDeleteIgnoreErrors      bool
	backupDeleteParallelProcesses int
)
var backupDeleteCmd = &cobra.Command{
	Use:   "backup-delete",
	Short: "Delete a specific existing backup",
	Long: `Delete a specific existing backup.

The --timestamp option must be specified. It could be specified multiple times.

By default, the existence of dependent backups is checked and deletion process is not performed,
unless the --cascade option is passed in.

If backup already deleted, the deletion process is skipped, unless --force option is specified.
If errors occur during the deletion process, the errors can be ignored using the --ignore-errors option.
The --ignore-errors option can be used only with --force option.

By default, the deletion will be performed for local backup.

The full path to the backup directory can be set using the --backup-dir option.

For local backups the following logic are applied:
  * If the --backup-dir option is specified, the deletion will be performed in provided path.
  * If the --backup-dir option is not specified, but the backup was made with --backup-dir flag for gpbackup, the deletion will be performed in the backup manifest path.
  * If the --backup-dir option is not specified and backup directory is not specified in backup manifest, the deletion will be performed in backup folder in the master and segments data directories.
  * If backup is not local, the error will be returned.

For control over the number of parallel processes and ssh connections to delete local backups, the --parallel-processes option can be used.

The storage plugin config file location can be set using the --plugin-config option.
The full path to the file is required. In this case, the deletion will be performed using the storage plugin.

For non local backups the following logic are applied:
  * If the --plugin-config option is specified, the deletion will be performed using the storage plugin.
  * If backup is local, the error will be returned.

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database will be searched in the current directory.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doRootFlagValidation(cmd.Flags(), checkFileExistsConst)
		doDeleteBackupFlagValidation(cmd.Flags())
		doDeleteBackup()
	},
}

var execCommand = exec.Command

func init() {
	rootCmd.AddCommand(backupDeleteCmd)
	backupDeleteCmd.PersistentFlags().StringArrayVar(
		&backupDeleteTimestamp,
		timestampFlagName,
		[]string{""},
		"the backup timestamp for deleting, could be specified multiple times",
	)
	backupDeleteCmd.PersistentFlags().StringVar(
		&backupDeletePluginConfigFile,
		pluginConfigFileFlagName,
		"",
		"the full path to plugin config file",
	)
	backupDeleteCmd.PersistentFlags().BoolVar(
		&backupDeleteCascade,
		cascadeFlagName,
		false,
		"delete all dependent backups for the specified backup timestamp",
	)
	backupDeleteCmd.PersistentFlags().BoolVar(
		&backupDeleteForce,
		forceFlagName,
		false,
		"try to delete, even if the backup already mark as deleted",
	)
	backupDeleteCmd.PersistentFlags().StringVar(
		&backupDeleteBackupDir,
		backupDirFlagName,
		"",
		"the full path to backup directory for local backups",
	)
	backupDeleteCmd.PersistentFlags().IntVar(
		&backupDeleteParallelProcesses,
		parallelProcessesFlagName,
		1,
		"the number of parallel processes to delete local backups",
	)
	backupDeleteCmd.PersistentFlags().BoolVar(
		&backupDeleteIgnoreErrors,
		ignoreErrorsFlagName,
		false,
		"ignore errors when deleting backups",
	)
	_ = backupDeleteCmd.MarkPersistentFlagRequired(timestampFlagName)
}

// These flag checks are applied only for backup-delete command.
func doDeleteBackupFlagValidation(flags *pflag.FlagSet) {
	var err error
	// If timestamps are specified and have correct values.
	if flags.Changed(timestampFlagName) {
		for _, timestamp := range backupDeleteTimestamp {
			err = gpbckpconfig.CheckTimestamp(timestamp)
			if err != nil {
				gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(timestamp, timestampFlagName, err))
				execOSExit(exitErrorCode)
			}
		}
	}
	// backup-dir anf plugin-config flags cannot be used together.
	err = checkCompatibleFlags(flags, backupDirFlagName, pluginConfigFileFlagName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableCompatibleFlags(err, backupDirFlagName, pluginConfigFileFlagName))
		execOSExit(exitErrorCode)
	}
	// If parallel-processes flag is specified and have correct values.
	if flags.Changed(parallelProcessesFlagName) && !gpbckpconfig.IsPositiveValue(backupDeleteParallelProcesses) {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(strconv.Itoa(backupDeleteParallelProcesses), parallelProcessesFlagName, err))
		execOSExit(exitErrorCode)
	}
	// plugin-config and parallel-precesses flags cannot be used together.
	err = checkCompatibleFlags(flags, parallelProcessesFlagName, pluginConfigFileFlagName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableCompatibleFlags(err, parallelProcessesFlagName, pluginConfigFileFlagName))
		execOSExit(exitErrorCode)
	}
	// If backup-dir flag is specified and it exists and the full path is specified.
	if flags.Changed(backupDirFlagName) {
		err = gpbckpconfig.CheckFullPath(backupDeleteBackupDir, checkFileExistsConst)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(backupDeleteBackupDir, backupDirFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	// If the plugin-config flag is specified and it exists and the full path is specified.
	if flags.Changed(pluginConfigFileFlagName) {
		err = gpbckpconfig.CheckFullPath(backupDeletePluginConfigFile, checkFileExistsConst)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(backupDeletePluginConfigFile, pluginConfigFileFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	// If ignore-errors flag is specified, but force flag is not.
	if flags.Changed(ignoreErrorsFlagName) && !flags.Changed(forceFlagName) {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateValue(textmsg.ErrorNotIndependentFlagsError(), ignoreErrorsFlagName, forceFlagName))
		execOSExit(exitErrorCode)
	}

}

func doDeleteBackup() {
	logHeadersDebug()
	err := deleteBackup()
	if err != nil {
		execOSExit(exitErrorCode)
	}
}

func deleteBackup() error {
	hDB, err := gpbckpconfig.OpenHistoryDB(getHistoryDBPath(rootHistoryDB))
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableActionHistoryDB("open", err))
		return err
	}
	defer func() {
		closeErr := hDB.Close()
		if closeErr != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableActionHistoryDB("close", closeErr))
		}
	}()
	if backupDeletePluginConfigFile != "" {
		pluginConfig, err := utils.ReadPluginConfig(backupDeletePluginConfigFile)
		if err != nil {
			return err
		}
		err = backupDeleteDBPlugin(backupDeleteTimestamp, backupDeleteCascade, backupDeleteForce, backupDeleteIgnoreErrors, backupDeletePluginConfigFile, pluginConfig, hDB)
		if err != nil {
			return err
		}
	} else {
		err := backupDeleteDBLocal(backupDeleteTimestamp, backupDeleteBackupDir, backupDeleteCascade, backupDeleteForce, backupDeleteIgnoreErrors, backupDeleteParallelProcesses, hDB)
		if err != nil {
			return err
		}
	}
	return nil
}

func backupDeleteDBPlugin(backupListForDeletion []string, deleteCascade, deleteForce, ignoreErrors bool, pluginConfigPath string, pluginConfig *utils.PluginConfig, hDB *sql.DB) error {
	deleter := &backupPluginDeleter{
		pluginConfigPath: pluginConfigPath,
		pluginConfig:     pluginConfig}
	// Skip local backups.
	skipLocalBackup := true
	return backupDeleteDB(backupListForDeletion, deleteCascade, deleteForce, ignoreErrors, skipLocalBackup, deleter, hDB)
}

func backupDeleteDBLocal(backupListForDeletion []string, backupDir string, deleteCascade, deleteForce, ignoreErrors bool, maxParallelProcesses int, hDB *sql.DB) error {
	deleter := &backupLocalDeleter{
		backupDir:            backupDir,
		maxParallelProcesses: maxParallelProcesses}
	// Include local backups.
	skipLocalBackups := false
	return backupDeleteDB(backupListForDeletion, deleteCascade, deleteForce, ignoreErrors, skipLocalBackups, deleter, hDB)
}

func backupDeleteDB(backupListForDeletion []string, deleteCascade, deleteForce, ignoreErrors, skipLocalBackup bool, deleter backupDeleteInterface, hDB *sql.DB) error {
	for _, backupName := range backupListForDeletion {
		backupData, err := gpbckpconfig.GetBackupDataDB(backupName, hDB)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableGetBackupInfo(backupName, err))
			return err
		}
		canBeDeleted, err := checkBackupCanBeUsed(deleteForce, skipLocalBackup, backupData)
		if err != nil {
			return err
		}
		if canBeDeleted {
			backupDependencies, err := gpbckpconfig.GetBackupDependencies(backupName, hDB)
			if err != nil {
				gplog.Error("%s", textmsg.ErrorTextUnableGetBackupValue("dependencies", backupName, err))
				return err
			}
			if len(backupDependencies) > 0 {
				gplog.Info("%s", textmsg.InfoTextBackupDependenciesList(backupName, backupDependencies))
				if deleteCascade {
					gplog.Debug("%s", textmsg.InfoTextBackupDeleteList(backupDependencies))
					// If the deletion of at least one dependent backup fails, we fail full entire chain.
					err = backupDeleteDBCascade(backupDependencies, deleteForce, ignoreErrors, skipLocalBackup, deleter, hDB)
					if err != nil {
						gplog.Error("%s", textmsg.ErrorTextUnableDeleteBackupCascade(backupName, err))
						return err
					}
				} else {
					gplog.Error("%s", textmsg.ErrorTextUnableDeleteBackupUseCascade(backupName, textmsg.ErrorBackupDeleteCascadeOptionError()))
					return textmsg.ErrorBackupDeleteCascadeOptionError()
				}
			}
			err = deleter.backupDeleteDB(backupName, hDB, ignoreErrors)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func backupDeleteDBCascade(backupList []string, deleteForce, ignoreErrors, skipLocalBackup bool, deleter backupDeleteInterface, hDB *sql.DB) error {
	for _, backup := range backupList {
		backupData, err := gpbckpconfig.GetBackupDataDB(backup, hDB)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableGetBackupInfo(backup, err))
			return err
		}
		// Skip local backup.
		canBeDeleted, err := checkBackupCanBeUsed(deleteForce, skipLocalBackup, backupData)
		if err != nil {
			return err
		}
		if canBeDeleted {
			err = deleter.backupDeleteDB(backup, hDB, ignoreErrors)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func backupDeleteDBPluginFunc(backupName, pluginConfigPath string, pluginConfig *utils.PluginConfig, hDB *sql.DB, ignoreErrors bool) error {
	var err error
	dateDeleted := history.CurrentTimestamp()
	gplog.Info("%s", textmsg.InfoTextBackupDeleteStart(backupName))
	err = gpbckpconfig.UpdateDeleteStatus(backupName, gpbckpconfig.DateDeletedInProgress, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableSetBackupStatus(gpbckpconfig.DateDeletedInProgress, backupName, err))
		return err
	}
	gplog.Debug("%s", textmsg.InfoTextCommandExecution(pluginConfig.ExecutablePath, deleteBackupPluginCommand, pluginConfigPath, backupName))
	stdout, stderr, errdel := execDeleteBackupPlugin(pluginConfig.ExecutablePath, deleteBackupPluginCommand, pluginConfigPath, backupName)
	if stderr != "" {
		gplog.Error("%s", stderr)
	}
	if errdel != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableDeleteBackup(backupName, errdel), gpbckpconfig.DateDeletedPluginFailed, hDB)
		if !ignoreErrors {
			return errdel
		}
	}
	gplog.Info("%s", stdout)
	backupData, err := gpbckpconfig.GetBackupDataDB(backupName, hDB)
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableGetBackupInfo(backupName, err), gpbckpconfig.DateDeletedPluginFailed, hDB)
		if !ignoreErrors {
			return err
		}
	}
	bckpDir, _, _, err := getBackupMasterDir("", backupData.BackupDir, backupData.DatabaseName)
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableGetBackupPath("backup directory", backupName, err), gpbckpconfig.DateDeletedPluginFailed, hDB)
		if !ignoreErrors {
			return err
		}
	}
	gplog.Debug("%s", textmsg.InfoTextCommandExecution("delete directory", gpbckpconfig.BackupDirPath(bckpDir, backupName)))
	// Delete local files on master.
	err = os.RemoveAll(gpbckpconfig.BackupDirPath(bckpDir, backupName))
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableDeleteBackup(backupName, err), gpbckpconfig.DateDeletedPluginFailed, hDB)
		if !ignoreErrors {
			return err
		}
	}
	err = gpbckpconfig.UpdateDeleteStatus(backupName, dateDeleted, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableSetBackupStatus(dateDeleted, backupName, err))
		return err
	}
	gplog.Info("%s", textmsg.InfoTextBackupDeleteSuccess(backupName))
	return nil
}

func backupDeleteDBLocalFunc(backupName, backupDir string, maxParallelProcesses int, hDB *sql.DB, ignoreErrors bool) error {
	var err, errUpdate error
	dateDeleted := history.CurrentTimestamp()
	gplog.Info("%s", textmsg.InfoTextBackupDeleteStart(backupName))
	errUpdate = gpbckpconfig.UpdateDeleteStatus(backupName, gpbckpconfig.DateDeletedInProgress, hDB)
	if errUpdate != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableSetBackupStatus(gpbckpconfig.DateDeletedInProgress, backupName, errUpdate))
		return errUpdate
	}
	backupData, err := gpbckpconfig.GetBackupDataDB(backupName, hDB)
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableGetBackupInfo(backupName, err), gpbckpconfig.DateDeletedLocalFailed, hDB)
		return err
	}
	bckpDir, segPrefix, isSingleBackupDir, err := getBackupMasterDir(backupDir, backupData.BackupDir, backupData.DatabaseName)
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableGetBackupPath("backup directory", backupName, err), gpbckpconfig.DateDeletedLocalFailed, hDB)
		return err
	}
	gplog.Debug("%s", textmsg.InfoTextBackupDirPath(bckpDir))
	gplog.Debug("%s", textmsg.InfoTextSegmentPrefix(segPrefix))
	backupType, err := gpbckpconfig.GetBackupType(backupData)
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableGetBackupValue("type", backupName, err), gpbckpconfig.DateDeletedLocalFailed, hDB)
		return err
	}
	// If backup type is not "metadata-only", we should delete files on segments and master.
	// If backup type is "metadata-only", we should not delete files only on master.
	if backupType != gpbckpconfig.BackupTypeMetadataOnly {
		var errSeg error
		segConfig, errSeg := getSegmentConfigurationClusterInfo(backupData.DatabaseName)
		if errSeg != nil {
			handleErrorDB(backupName, textmsg.ErrorTextUnableGetBackupPath("segment configuration", backupName, errSeg), gpbckpconfig.DateDeletedLocalFailed, hDB)
			if !ignoreErrors {
				return errSeg
			}
		}
		// Execute on segments.
		errSeg = executeDeleteBackupOnSegments(backupDir, backupData.BackupDir, backupName, segPrefix, isSingleBackupDir, ignoreErrors, segConfig, maxParallelProcesses)
		if errSeg != nil {
			handleErrorDB(backupName, textmsg.ErrorTextUnableDeleteBackup(backupName, errSeg), gpbckpconfig.DateDeletedLocalFailed, hDB)
			if !ignoreErrors {
				return errSeg
			}
		}
	}
	// Delete files on master.
	gplog.Debug("%s", textmsg.InfoTextCommandExecution("delete directory", gpbckpconfig.BackupDirPath(bckpDir, backupName)))
	err = os.RemoveAll(gpbckpconfig.BackupDirPath(bckpDir, backupName))
	if err != nil {
		handleErrorDB(backupName, textmsg.ErrorTextUnableDeleteBackup(backupName, err), gpbckpconfig.DateDeletedLocalFailed, hDB)
		if !ignoreErrors {
			return err
		}
	}
	errUpdate = gpbckpconfig.UpdateDeleteStatus(backupName, dateDeleted, hDB)
	if errUpdate != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableSetBackupStatus(dateDeleted, backupName, errUpdate))
		return errUpdate
	}
	gplog.Info("%s", textmsg.InfoTextBackupDeleteSuccess(backupName))
	return nil
}

func execDeleteBackupPlugin(executablePath, deleteBackupPluginCommand, pluginConfigFile, timestamp string) (string, string, error) {
	cmd := execCommand(executablePath, deleteBackupPluginCommand, pluginConfigFile, timestamp)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// ExecuteCommandsOnHosts Delete backup dir on all segment hosts in parallel.
// The function checks that the directories exists on all segment hosts before deletion.
func executeDeleteBackupOnSegments(backupDir, backupDataBackupDir, backupName, segPrefix string, isSingleBackupDir, ignoreErrors bool, configs []gpbckpconfig.SegmentConfig, maxParallelProcesses int) error {
	var once sync.Once
	limit := make(chan bool, maxParallelProcesses)
	wg := &sync.WaitGroup{}
	errCh := make(chan error, len(configs))
	sshClientConf, err := getSSHConfig()
	if err != nil {
		return err
	}
	// Check that the directory exists on all segment hosts.
	for _, config := range configs {
		wg.Add(1)
		limit <- true
		backupPath, err := getBackupSegmentDir(backupDir, backupDataBackupDir, config.DataDir, segPrefix, config.ContentID, isSingleBackupDir)
		if err != nil {
			return err
		}
		go func(backupPath, host string) {
			defer func() { <-limit }()
			defer wg.Done()
			checkBackupDirExistsOnSegments(gpbckpconfig.BackupDirPath(backupPath, backupName), host, sshClientConf, errCh)
		}(backupPath, config.Hostname)
	}
	// We should block the main function and wait for the WaitGroup to complete.
	// It is necessary to strictly verify that all checks are performed for a specific backup
	// and this particular backup will be deleted.
	// Only after deleting backups can we move on to the next one.
	// It is necessary to avoid situations where checks are performed simultaneously for one backup,
	// and deletion occurs for another.
	//
	// Don't use code like
	// go func() { wg.Wait(); once.Do(func() { close(errCh) }) }()
	//
	wg.Wait()
	// Fix error like "panic: close of closed channel".
	once.Do(func() {
		close(errCh)
	})
	for err := range errCh {
		if err != nil && !ignoreErrors {
			return err
		}
	}
	// If all checks passed, delete the directory on all segment hosts.
	// Reset the wait group.
	wg = &sync.WaitGroup{}
	for _, config := range configs {
		wg.Add(1)
		limit <- true
		backupPath, err := getBackupSegmentDir(backupDir, backupDataBackupDir, config.DataDir, segPrefix, config.ContentID, isSingleBackupDir)
		if err != nil {
			return err
		}
		go func(backupPath, host string) {
			defer func() { <-limit }()
			defer wg.Done()
			deleteBackupDirOnSegments(gpbckpconfig.BackupDirPath(backupPath, backupName), host, sshClientConf, errCh)
		}(backupPath, config.Hostname)
	}
	wg.Wait()
	// Fix error like "panic: close of closed channel".
	once.Do(func() {
		close(errCh)
	})
	for err := range errCh {
		if err != nil && !ignoreErrors {
			return err
		}
	}
	return nil
}
func checkBackupDirExistsOnSegments(path, host string, sshConf *ssh.ClientConfig, errCh chan error) {
	connection, err := ssh.Dial("tcp", host+":22", sshConf)
	if err != nil {
		errCh <- err
		return
	}
	defer connection.Close()

	session, err := connection.NewSession()
	if err != nil {
		errCh <- err
		return
	}
	defer session.Close()
	command := fmt.Sprintf("test -d %s", path)
	gplog.Debug("%s", textmsg.InfoTextCommandExecution(command, "on host", host))
	if err := session.Run(command); err != nil {
		gplog.Error("%s", textmsg.ErrorTextCommandExecutionFailed(err, command, "on host", host))
		errCh <- textmsg.ErrorNotFoundBackupDirIn(fmt.Sprintf("%s on host %s", path, host))
		return
	}
	gplog.Debug("%s", textmsg.InfoTextCommandExecutionSucceeded(command, "on host", host))
}

func deleteBackupDirOnSegments(path, host string, sshConf *ssh.ClientConfig, errCh chan error) {
	connection, err := ssh.Dial("tcp", host+":22", sshConf)
	if err != nil {
		errCh <- err
		return
	}
	defer connection.Close()

	session, err := connection.NewSession()
	if err != nil {
		errCh <- err
		return
	}
	defer session.Close()
	command := fmt.Sprintf("rm -rf %s", path)
	gplog.Debug("%s", textmsg.InfoTextCommandExecution(command, "on host", host))
	if err := session.Run(command); err != nil {
		gplog.Error("%s", textmsg.ErrorTextCommandExecutionFailed(err, command, "on host", host))
		errCh <- err
		return
	}
	gplog.Debug("%s", textmsg.InfoTextCommandExecutionSucceeded(command, "on host", host))
}

func getSSHConfig() (*ssh.ClientConfig, error) {
	currentUser, _ := operating.System.CurrentUser()
	key, err := os.ReadFile(currentUser.HomeDir + "/.ssh/id_rsa")
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	// sshConfig is a configuration object for establishing an SSH connection.
	// It contains the user's username, authentication method using public keys,
	// and a host key callback that ignores insecure host keys.
	sshConfig := &ssh.ClientConfig{
		User: currentUser.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// Disable known_hosts check.
		// This check also disables in gpbackup utility.
		// #nosec G106
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	return sshConfig, nil
}
