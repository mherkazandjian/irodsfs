package irodsfs

import (
	"context"
	"os"
	"sync"
	"syscall"

	irodsclient_fs "github.com/cyverse/go-irodsclient/fs"
	irodsclient_types "github.com/cyverse/go-irodsclient/irods/types"
	irodsfs_common_utils "github.com/cyverse/irodsfs-common/utils"
	fusefs "github.com/hanwen/go-fuse/v2/fs"
	fuse "github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/xerrors"

	log "github.com/sirupsen/logrus"
)

// File is a file node
type File struct {
	fusefs.Inode

	fs      *IRODSFS
	entryID int64
	path    string
	mutex   sync.RWMutex
}

// NewFile creates a new File
func NewFile(fs *IRODSFS, entryID int64, path string) *File {
	return &File{
		fs:      fs,
		entryID: entryID,
		path:    path,
		mutex:   sync.RWMutex{},
	}
}

func (file *File) getStableAttr() fusefs.StableAttr {
	return fusefs.StableAttr{
		Mode: fuse.S_IFREG,
		Ino:  getInodeIDFromEntryID(file.entryID),
		Gen:  0,
	}
}

func (file *File) setAttrOutForIRODSEntry(entry *irodsclient_fs.Entry, readonly bool, out *fuse.Attr) {
	mode := GetACL(file.fs, entry, readonly)
	setAttrOutForIRODSEntry(entry, file.fs.uid, file.fs.gid, mode, out)
}

// Getattr returns stat of file entry
func (file *File) Getattr(ctx context.Context, fh fusefs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Getattr",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Getattr (%d) - %s", operID, file.path)
	defer logger.Infof("Called Getattr (%d) - %s", operID, file.path)

	file.mutex.RLock()
	defer file.mutex.RUnlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		logger.Errorf("failed to get file attribute from a virtual dir mapping")
		return syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	_, irodsEntry, err := vpathEntry.StatIRODSEntry(file.fs.fsClient, file.path)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", file.path)
			return syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	file.setAttrOutForIRODSEntry(irodsEntry, vpathEntry.ReadOnly, &out.Attr)
	return fusefs.OK
}

// Setattr sets file attributes
func (file *File) Setattr(ctx context.Context, fh fusefs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Setattr",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Setattr (%d) - %s", operID, file.path)
	defer logger.Infof("Called Setattr (%d) - %s", operID, file.path)

	// do not return EOPNOTSUPP as it causes client errors, like git clone
	/*
		if _, ok := in.GetMode(); ok {
			// chmod
			// not supported
			return syscall.EOPNOTSUPP
		} else if _, ok := in.GetATime(); ok {
			// changing date
			// not supported but return OK to not cause various errors in linux commands
			return fusefs.OK
		} else if _, ok := in.GetCTime(); ok {
			// changing date
			// not supported but return OK to not cause various errors in linux commands
			return fusefs.OK
		} else if _, ok := in.GetMTime(); ok {
			// changing date
			// not supported but return OK to not cause various errors in linux commands
			return fusefs.OK
		} else if _, ok := in.GetGID(); ok {
			// changing ownership
			// not supported
			return syscall.EOPNOTSUPP
		} else if _, ok := in.GetUID(); ok {
			// changing ownership
			// not supported
			return syscall.EOPNOTSUPP
		}
	*/
	if size, ok := in.GetSize(); ok {
		// truncate file
		errno := file.Truncate(ctx, size)
		if errno != fusefs.OK {
			return errno
		}

		out.Size = size
		return fusefs.OK
	}

	return fusefs.OK
}

// Listxattr lists xattr
// read all attributes (null terminated) into
// `dest`. If the `dest` buffer is too small, it should return ERANGE
// and the correct size.  If not defined, return an empty list and
// success.
func (file *File) Listxattr(ctx context.Context, dest []byte) (uint32, syscall.Errno) {
	if file.fs.terminated {
		return 0, syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Listxattr",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Listxattr (%d) - %s", operID, file.path)
	defer logger.Infof("Called Listxattr (%d) - %s", operID, file.path)

	file.mutex.RLock()
	defer file.mutex.RUnlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return 0, syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		logger.Errorf("failed to get file extended attribute from a virtual dir mapping")
		return 0, syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return 0, syscall.EREMOTEIO
	}

	irodsPath, err := vpathEntry.GetIRODSPath(file.path)
	if err != nil {
		logger.Errorf("%+v", err)
		return 0, syscall.EREMOTEIO
	}

	irodsMetadata, err := file.fs.fsClient.ListXattr(irodsPath)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", irodsPath)
			return 0, syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return 0, syscall.EREMOTEIO
	}

	// convert to a byte array
	xattrNames := []byte{}
	for _, irodsMeta := range irodsMetadata {
		xattrNames = append(xattrNames, []byte(irodsMeta.Name)...)
		xattrNames = append(xattrNames, byte(0))
	}

	requiredBytesLen := len(xattrNames)
	if len(dest) < requiredBytesLen {
		return uint32(requiredBytesLen), syscall.ERANGE
	}

	// has any?
	if len(xattrNames) > 0 {
		copy(dest, xattrNames)
		return uint32(requiredBytesLen), fusefs.OK
	}

	// return empty
	return 0, fusefs.OK
}

// Getxattr returns xattr
// return the number of bytes. If `dest` is too
// small, it should return ERANGE and the size of the attribute.
// If not defined, Getxattr will return ENOATTR.
func (file *File) Getxattr(ctx context.Context, attr string, dest []byte) (uint32, syscall.Errno) {
	if file.fs.terminated {
		return 0, syscall.ECONNABORTED
	}

	if IsUnhandledAttr(attr) {
		return 0, syscall.ENODATA
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Getxattr",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Getxattr (%d) - %s, attr %s", operID, file.path, attr)
	defer logger.Infof("Called Getxattr (%d) - %s, attr %s", operID, file.path, attr)

	file.mutex.RLock()
	defer file.mutex.RUnlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return 0, syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		logger.Errorf("failed to get file extended attribute from a virtual dir mapping")
		return 0, syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return 0, syscall.EREMOTEIO
	}

	irodsPath, err := vpathEntry.GetIRODSPath(file.path)
	if err != nil {
		logger.Errorf("%+v", err)
		return 0, syscall.EREMOTEIO
	}

	irodsMeta, err := file.fs.fsClient.GetXattr(irodsPath, attr)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", irodsPath)
			return 0, syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return 0, syscall.EREMOTEIO
	}

	if irodsMeta == nil {
		return 0, syscall.ENODATA
	}

	requiredBytesLen := len([]byte(irodsMeta.Value))

	if len(dest) < requiredBytesLen {
		return uint32(requiredBytesLen), syscall.ERANGE
	}

	copy(dest, []byte(irodsMeta.Value))
	return uint32(requiredBytesLen), fusefs.OK
}

// Setxattr sets xattr
// If not defined, Setxattr will return ENOATTR.
func (file *File) Setxattr(ctx context.Context, attr string, data []byte, flags uint32) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Setxattr",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Setxattr (%d) - %s", operID, file.path)
	defer logger.Infof("Called Setxattr (%d) - %s", operID, file.path)

	if IsUnhandledAttr(attr) {
		return syscall.EINVAL
	}

	file.mutex.RLock()
	defer file.mutex.RUnlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		logger.Errorf("failed to set file extended attribute from a virtual dir mapping")
		return syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	irodsPath, err := vpathEntry.GetIRODSPath(file.path)
	if err != nil {
		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	logger.Debugf("xattr %s - '%v'", irodsPath, data)

	err = file.fs.fsClient.SetXattr(irodsPath, attr, string(data))
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", irodsPath)
			return syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	return fusefs.OK
}

// Removexattr removes xattr
// If not defined, Removexattr will return ENOATTR.
func (file *File) Removexattr(ctx context.Context, attr string) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Removexattr",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Removexattr (%d) - %s", operID, file.path)
	defer logger.Infof("Called Removexattr (%d) - %s", operID, file.path)

	file.mutex.RLock()
	defer file.mutex.RUnlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		logger.Errorf("failed to remove file extended attribute from a virtual dir mapping")
		return syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	irodsPath, err := vpathEntry.GetIRODSPath(file.path)
	if err != nil {
		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	irodsMeta, err := file.fs.fsClient.GetXattr(irodsPath, attr)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", irodsPath)
			return syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	if irodsMeta == nil {
		return syscall.ENODATA
	}

	err = file.fs.fsClient.RemoveXattr(irodsPath, attr)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", irodsPath)
			return syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	return fusefs.OK
}

// Truncate truncates file entry
func (file *File) Truncate(ctx context.Context, size uint64) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Truncate",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Truncate (%d) - %s, %d", operID, file.path, size)
	defer logger.Infof("Called Truncate (%d) - %s, %d", operID, file.path, size)

	file.mutex.Lock()
	defer file.mutex.Unlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		logger.Errorf("failed to truncate a virtual dir")
		return syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	_, irodsEntry, err := vpathEntry.StatIRODSEntry(file.fs.fsClient, file.path)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", file.path)
			return syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return syscall.EREMOTEIO
	}

	// check if there're opened file handles
	// handle ftruncate operation
	callFtruncate := false
	handlesOpened := file.fs.fileHandleMap.ListByPath(irodsEntry.Path)
	for _, handle := range handlesOpened {
		if handle.fileHandle.IsWriteMode() {
			// is writing
			logger.Infof("Found opened file handle %s - %s", handle.file.path, handle.fileHandle.GetID())

			errno := handle.Truncate(ctx, size)
			if errno != 0 {
				logger.Errorf("failed to truncate a file - %s, %d", irodsEntry.Path, size)
				return errno
			}

			callFtruncate = true

			// avoid truncating a file multiple times
			break
		}
	}

	if !callFtruncate {
		if irodsEntry.Size != int64(size) {
			err = file.fs.fsClient.TruncateFile(irodsEntry.Path, int64(size))
			if err != nil {
				if irodsclient_types.IsFileNotFoundError(err) {
					logger.Debugf("failed to find a file - %s", irodsEntry.Path)
					return syscall.ENOENT
				}

				logger.Errorf("%+v", err)
				return syscall.EREMOTEIO
			}
		}
	}

	return fusefs.OK
}

// Open opens file for the path and returns file handle
func (file *File) Open(ctx context.Context, flags uint32) (fusefs.FileHandle, uint32, syscall.Errno) {
	if file.fs.terminated {
		return nil, 0, syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Open",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	openMode := string(irodsclient_types.FileOpenModeReadWrite)
	fuseFlag := uint32(0)

	// if we use Direct_IO, it will disable kernel cache, read-ahead, shared mmap
	//fuseFlag |= fuse.FOPEN_DIRECT_IO

	if flags&uint32(os.O_WRONLY) == uint32(os.O_WRONLY) {
		openMode = string(irodsclient_types.FileOpenModeWriteOnly)

		if flags&uint32(os.O_APPEND) == uint32(os.O_APPEND) {
			// append
			openMode = string(irodsclient_types.FileOpenModeAppend)
		} else if flags&uint32(os.O_TRUNC) == uint32(os.O_TRUNC) {
			// truncate
			openMode = string(irodsclient_types.FileOpenModeWriteTruncate)
		}
	} else if flags&uint32(os.O_RDWR) == uint32(os.O_RDWR) {
		openMode = string(irodsclient_types.FileOpenModeReadWrite)
	} else {
		openMode = string(irodsclient_types.FileOpenModeReadOnly)
		//fuseFlag |= fuse.FOPEN_KEEP_CACHE
	}

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Open (%d) - %s, mode(%s)", operID, file.path, openMode)
	defer logger.Infof("Called Open (%d) - %s, mode(%s)", operID, file.path, openMode)

	file.mutex.RLock()
	defer file.mutex.RUnlock()

	vpathEntry := file.fs.vpathManager.GetClosestEntry(file.path)
	if vpathEntry == nil {
		logger.Errorf("failed to get VPath Entry for %s", file.path)
		return nil, 0, syscall.EREMOTEIO
	}

	// Virtual Dir
	if vpathEntry.IsVirtualDirEntry() {
		// failed to open directory
		err := xerrors.Errorf("failed to open mapped directory entry - %s", vpathEntry.Path)
		logger.Error(err)
		return nil, 0, syscall.EPERM
	}

	if vpathEntry.ReadOnly && openMode != string(irodsclient_types.FileOpenModeReadOnly) {
		logger.Errorf("failed to open a read-only file with non-read-only mode")
		return nil, 0, syscall.EREMOTEIO
	}

	// IRODS Dir
	err := ensureVPathEntryIsIRODSEntry(file.fs.fsClient, vpathEntry)
	if err != nil {
		logger.Errorf("%+v", err)
		return nil, 0, syscall.EREMOTEIO
	}

	irodsPath, err := vpathEntry.GetIRODSPath(file.path)
	if err != nil {
		logger.Errorf("%+v", err)
		return nil, 0, syscall.EREMOTEIO
	}

	handle, err := file.fs.fsClient.OpenFile(irodsPath, "", openMode)
	if err != nil {
		if irodsclient_types.IsFileNotFoundError(err) {
			logger.Debugf("failed to find a file - %s", irodsPath)
			return nil, 0, syscall.ENOENT
		}

		logger.Errorf("%+v", err)
		return nil, 0, syscall.EREMOTEIO
	}

	if file.fs.instanceReportClient != nil {
		file.fs.instanceReportClient.StartFileAccess(handle)
	}

	fileHandle, err := NewFileHandle(file, handle)
	if err != nil {
		logger.Errorf("%+v", err)
		return nil, 0, syscall.EREMOTEIO
	}

	// add to file handle map
	file.fs.fileHandleMap.Add(fileHandle)

	return fileHandle, fuseFlag, fusefs.OK
}

// Getlk returns locks
func (file *File) Getlk(ctx context.Context, fh fusefs.FileHandle, owner uint64, lk *fuse.FileLock, flags uint32, out *fuse.FileLock) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Getlk",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Getattr (%d) - %s", operID, file.path)
	defer logger.Infof("Called Getattr (%d) - %s", operID, file.path)

	fileHandle, ok := fh.(*FileHandle)
	if !ok {
		logger.Errorf("failed to convert fh to a file handle - %s", fileHandle.file.path)
		return syscall.EREMOTEIO
	}

	if fileHandle.fileHandle == nil {
		logger.Errorf("failed to get a file handle - %s", fileHandle.file.path)
		return syscall.EREMOTEIO
	}

	return fileHandle.GetLocalLock(ctx, owner, lk, flags, out)
}

// Setlk obtains a lock on a file, or fail if the lock could not obtained
func (file *File) Setlk(ctx context.Context, fh fusefs.FileHandle, owner uint64, lk *fuse.FileLock, flags uint32) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Setlk",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Setlk (%d) - %s", operID, file.path)
	defer logger.Infof("Called Setlk (%d) - %s", operID, file.path)

	fileHandle, ok := fh.(*FileHandle)
	if !ok {
		logger.Errorf("failed to convert fh to a file handle - %s", fileHandle.file.path)
		return syscall.EREMOTEIO
	}

	if fileHandle.fileHandle == nil {
		logger.Errorf("failed to get a file handle - %s", fileHandle.file.path)
		return syscall.EREMOTEIO
	}

	return fileHandle.SetLocalLock(ctx, owner, lk, flags)
}

// Setlkw obtains a lock on a file, waiting if necessary
func (file *File) Setlkw(ctx context.Context, fh fusefs.FileHandle, owner uint64, lk *fuse.FileLock, flags uint32) syscall.Errno {
	if file.fs.terminated {
		return syscall.ECONNABORTED
	}

	logger := log.WithFields(log.Fields{
		"package":  "irodsfs",
		"struct":   "File",
		"function": "Setlkw",
	})

	defer irodsfs_common_utils.StackTraceFromPanic(logger)

	operID := file.fs.GetNextOperationID()
	logger.Infof("Calling Setlkw (%d) - %s", operID, file.path)
	defer logger.Infof("Called Setlkw (%d) - %s", operID, file.path)

	fileHandle, ok := fh.(*FileHandle)
	if !ok {
		logger.Errorf("failed to convert fh to a file handle - %s", fileHandle.file.path)
		return syscall.EREMOTEIO
	}

	if fileHandle.fileHandle == nil {
		logger.Errorf("failed to get a file handle - %s", fileHandle.file.path)
		return syscall.EREMOTEIO
	}

	return fileHandle.SetLocalLockW(ctx, owner, lk, flags)
}

/*
func (dir *Dir) Link(ctx context.Context, target InodeEmbedder, name string, out *fuse.EntryOut) (node *Inode, errno syscall.Errno) {
}

func (dir *Dir) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (node *Inode, errno syscall.Errno) {
}

func (dir *Dir) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
}
*/
