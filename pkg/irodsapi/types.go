package irodsapi

import (
	"fmt"
	"time"
)

// EntryType defines types of Entry
type EntryType string

const (
	// FileEntry is a Entry type for a file
	FileEntry EntryType = "file"
	// DirectoryEntry is a Entry type for a directory
	DirectoryEntry EntryType = "directory"
)

type IRODSEntry struct {
	ID         int64
	Type       EntryType
	Name       string
	Path       string
	Owner      string
	Size       int64
	CreateTime time.Time
	ModifyTime time.Time
	CheckSum   string
}

type IRODSAccessLevelType string

const (
	// IRODSAccessLevelOwner is for owner access
	IRODSAccessLevelOwner IRODSAccessLevelType = "own"
	// IRODSAccessLevelWrite is for write access
	IRODSAccessLevelWrite IRODSAccessLevelType = "modify object"
	// IRODSAccessLevelRead is for read access
	IRODSAccessLevelRead IRODSAccessLevelType = "read object"
	// IRODSAccessLevelNone is for no access
	IRODSAccessLevelNone IRODSAccessLevelType = ""
)

type IRODSAccess struct {
	UserName    string
	AccessLevel IRODSAccessLevelType
}

// FileOpenMode determines file open mode
type FileOpenMode string

const (
	// FileOpenModeReadOnly is for read
	FileOpenModeReadOnly FileOpenMode = "r"
	// FileOpenModeReadWrite is for read and write
	FileOpenModeReadWrite FileOpenMode = "r+"
	// FileOpenModeWriteOnly is for write
	FileOpenModeWriteOnly FileOpenMode = "w"
	// FileOpenModeWriteTruncate is for write, but truncates the file
	FileOpenModeWriteTruncate FileOpenMode = "w+"
	// FileOpenModeAppend is for write, not trucate, but appends from the file end
	FileOpenModeAppend FileOpenMode = "a"
	// FileOpenModeReadAppend is for read and write, but appends from the file end
	FileOpenModeReadAppend FileOpenMode = "a+"
)

// Whence determines where to start counting the offset
type Whence int

const (
	// SeekSet means offset starts from file start
	SeekSet Whence = 0
	// SeekCur means offset starts from current offset
	SeekCur Whence = 1
	// SeekEnd means offset starts from file end
	SeekEnd Whence = 2
)

// FileNotFoundError ...
type FileNotFoundError struct {
	message string
}

// NewFileNotFoundError creates FileNotFoundError struct
func NewFileNotFoundError(message string) *FileNotFoundError {
	return &FileNotFoundError{
		message: message,
	}
}

// NewFileNotFoundErrorf creates FileNotFoundError struct
func NewFileNotFoundErrorf(format string, v ...interface{}) *FileNotFoundError {
	return &FileNotFoundError{
		message: fmt.Sprintf(format, v...),
	}
}

func (e *FileNotFoundError) Error() string {
	return e.message
}

// IsFileNotFoundError evaluates if the given error is FileNotFoundError
func IsFileNotFoundError(err error) bool {
	if _, ok := err.(*FileNotFoundError); ok {
		return true
	}
	return false
}
