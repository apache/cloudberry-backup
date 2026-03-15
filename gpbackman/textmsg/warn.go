package textmsg

import "fmt"

func WarnTextBackupUnableGetReport(backupName string) string {
	return fmt.Sprintf("Unable to get report for backup %s. Check if backup is active", backupName)
}
