package acbrun

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func ExtractTarGz(gzipStream io.Reader, dst string) (err error) {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return err
	}

	tarReader := tar.NewReader(uncompressedStream)

	hardLinks := make(map[string]string)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(filepath.Join(dst, header.Name), header.FileInfo().Mode()); err != nil {
				if !errors.Is(err, os.ErrExist) {
					return err
				}
			}
		case tar.TypeReg:
			outFile, err := os.OpenFile(filepath.Join(dst, header.Name), os.O_RDWR|os.O_CREATE|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			defer func() {
				err2 := outFile.Close()
				if err == nil {
					err = err2
				}
			}()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return err
			}
		case tar.TypeLink:
			hardLinks[filepath.Join(dst, header.Name)] = filepath.Join(dst, header.Linkname)
		case tar.TypeSymlink:
			err := os.Symlink(header.Linkname, filepath.Join(dst, header.Name))
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf(
				"ExtractTarGz: uknown type: %v in %s",
				header.Typeflag,
				header.Name)
		}
	}
	for k, v := range hardLinks {
		if err := os.Link(v, k); err != nil {
			return err
		}
	}
	return nil
}

func CreateTarGz(srcDir string, buf io.Writer) error {
	gw := gzip.NewWriter(buf)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	absSrcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}

	filepath.WalkDir(absSrcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(absSrcDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()

		var link string
		if mode&os.ModeSymlink != 0 {
			var err error
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = relPath
		err = tw.WriteHeader(h)
		if err != nil {
			return err
		}
		if mode.IsRegular() {
			fp, err := os.Open(path)
			if err != nil {
				return err
			}
			defer fp.Close()
			_, err = io.Copy(tw, fp)
			if err != nil {
				return err
			}
		}
		return nil
	})

	return nil
}

func addFileToArchive(tw *tar.Writer, workingDir, path string) error {
	file, err := os.Open(filepath.Join(workingDir, path))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = path
	err = tw.WriteHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(tw, file)
	if err != nil {
		return err
	}
	return nil
}
