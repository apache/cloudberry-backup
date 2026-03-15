package textmsg

import (
	"fmt"
	"strings"
)

func InfoTextBackupDeleteStart(backupName string) string {
	return fmt.Sprintf("Start deleting backup %s", backupName)
}

func InfoTextBackupAlreadyDeleted(backupName string) string {
	return fmt.Sprintf("Backup %s has already been deleted", backupName)
}

func InfoTextBackupStatus(backupName, backupStatus string) string {
	return fmt.Sprintf("Backup %s has status: %s", backupName, backupStatus)
}

func InfoTextBackupDeleteSuccess(backupName string) string {
	return fmt.Sprintf("Backup %s successfully deleted", backupName)
}

func InfoTextBackupDependenciesList(backupName string, list []string) string {
	return fmt.Sprintf("Backup %s has dependent backups: %s", backupName, strings.Join(list, ", "))
}

func InfoTextBackupDeleteList(list []string) string {
	return fmt.Sprintf("The following backups will be deleted: %s", strings.Join(list, ", "))
}

func InfoTextBackupDeleteListFromHistory(list []string) string {
	return fmt.Sprintf("The following backups will be deleted from history: %s", strings.Join(list, ", "))
}

func InfoTextCommandExecution(list ...string) string {
	return fmt.Sprintf("Executing command: %s", strings.Join(list, " "))
}

func InfoTextCommandExecutionSucceeded(list ...string) string {
	return fmt.Sprintf("Command succeeded: %s", strings.Join(list, " "))
}

func InfoTextBackupDirPath(backupDir string) string {
	return fmt.Sprintf("Path to backup directory: %s", backupDir)
}

func InfoTextSegmentPrefix(segPrefix string) string {
	return fmt.Sprintf("Segment Prefix: %s", segPrefix)
}

func InfoTextNothingToDo() string {
	return "Nothing to do"
}
