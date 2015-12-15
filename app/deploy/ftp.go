package deploy

import (
	"errors"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-xiaohei/pugo-static/app/builder"
	"github.com/jlaffaye/ftp"
	"gopkg.in/inconshreveable/log15.v2"
	"gopkg.in/ini.v1"
)

const (
	TYPE_FTP = "ftp"
)

var (
	_ DeployTask = new(FtpTask)
)

type (
	// ftp deployment task
	FtpTask struct {
		name string
		opt  *FtpOption
	}
	// ftp deploy option
	FtpOption struct {
		url      *url.URL
		Address  string `ini:"address"`
		User     string `ini:"user"`
		Password string `ini:"password"`
	}
)

// is option valid
func (fopt *FtpOption) isValid() error {
	if fopt.Address == "" || fopt.User == "" || fopt.Password == "" {
		return errors.New("deploy to ft need addres, username and password")
	}
	var err error
	fopt.url, err = url.Parse(fopt.Address)
	return err
}

// new ftp task with section
func (ft *FtpTask) New(name string, section *ini.Section) (DeployTask, error) {
	var (
		f = &FtpTask{
			name: name,
			opt:  &FtpOption{},
		}
		err error
	)
	if err = section.MapTo(f.opt); err != nil {
		return nil, err
	}
	if err = f.IsValid(); err != nil {
		return nil, err
	}
	return f, nil
}

// ftp task's name
func (ft *FtpTask) Name() string {
	return ft.name
}

// ftp task's is valid
func (ft *FtpTask) IsValid() error {
	return ft.opt.isValid()
}

// ftp task's type
func (ft *FtpTask) Type() string {
	return TYPE_FTP
}

// ftp task do action
func (ft *FtpTask) Do(b *builder.Builder, ctx *builder.Context) error {
	client, err := ftp.DialTimeout(ft.opt.url.Host, time.Second*10)
	if err != nil {
		return err
	}
	log15.Debug("Deploy.[" + ft.opt.Address + "].Connect")
	defer client.Quit()
	if ft.opt.User != "" {
		if err = client.Login(ft.opt.User, ft.opt.Password); err != nil {
			return err
		}
	}
	log15.Debug("Deploy.[" + ft.opt.Address + "].Logged")
	ftpDir := strings.TrimPrefix(ft.opt.url.Path, "/")

	// make dir
	makeFtpDir(client, getDirs(ftpDir))

	// change  to directory
	if err = client.ChangeDir(ftpDir); err != nil {
		return err
	}

	if b.Count < 2 {
		log15.Debug("Deploy.[" + ft.opt.Address + "].UploadAll")
		return ft.uploadAllFiles(client, ctx)
	}

	log15.Debug("Deploy.[" + ft.opt.Address + "].UploadDiff")
	return ft.uploadDiffFiles(client, ctx)
}

// upload files with checking diff status
func (ft *FtpTask) uploadDiffFiles(client *ftp.ServerConn, ctx *builder.Context) error {
	return ctx.Diff.Walk(func(name string, entry *builder.DiffEntry) error {
		rel, _ := filepath.Rel(ctx.DstDir, name)
		rel = filepath.ToSlash(rel)

		if entry.Behavior == builder.DIFF_REMOVE {
			log15.Debug("Deploy.Ftp.Delete", "file", rel)
			return client.Delete(rel)
		}

		if entry.Behavior == builder.DIFF_KEEP {
			if list, _ := client.List(rel); len(list) == 1 {
				// entry file should be older than uploaded file
				if entry.Time.Sub(list[0].Time).Seconds() < 0 {
					return nil
				}
			}
		}

		dirs := getDirs(path.Dir(rel))
		for i := len(dirs) - 1; i >= 0; i-- {
			client.MakeDir(dirs[i])
		}

		// upload file
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		if err = client.Stor(rel, f); err != nil {
			return err
		}
		log15.Debug("Deploy.Ftp.Stor", "file", rel)
		return nil
	})
}

// upload all files ignore diff status
func (ft *FtpTask) uploadAllFiles(client *ftp.ServerConn, ctx *builder.Context) error {
	var (
		createdDirs = make(map[string]bool)
		err         error
	)
	return ctx.Diff.Walk(func(name string, entry *builder.DiffEntry) error {
		rel, _ := filepath.Rel(ctx.DstDir, name)
		rel = filepath.ToSlash(rel)

		// entry remove status, just remove it
		// the other files, just upload ignore diff status
		if entry.Behavior == builder.DIFF_REMOVE {
			log15.Debug("Deploy.Ftp.Delete", "file", rel)
			return client.Delete(rel)
		}

		// create directory recursive
		dirs := getDirs(path.Dir(rel))
		if len(dirs) > 0 {
			for i := len(dirs) - 1; i >= 0; i-- {
				dir := dirs[i]
				if !createdDirs[dir] {
					if err = client.MakeDir(dir); err == nil {
						createdDirs[dir] = true
					}
				}
			}
		}

		// upload file
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		if err = client.Stor(rel, f); err != nil {
			return err
		}
		log15.Debug("Deploy.Ftp.Stor", "file", rel)
		return nil
	})
}

func getDirs(dir string) []string {
	if dir == "." || dir == "/" {
		return nil
	}
	dirs := []string{dir}
	for {
		dir = path.Dir(dir)
		if dir == "." || dir == "/" {
			break
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

func makeFtpDir(client *ftp.ServerConn, dirs []string) error {
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := client.MakeDir(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}