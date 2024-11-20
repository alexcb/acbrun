package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/tidwall/sjson"
)

//go:embed config.json
var configJSON string

var opts struct {
	// Slice of bool will append 'true' each time the option
	// is encountered (can be set multiple times, like -vvv)
	Verbose []bool `short:"v" long:"verbose" description:"Show verbose debug information"`
	Keep    bool   `long:"keep" description:"Keep temporary working directory"`
}

func ExtractTarGz(gzipStream io.Reader, dst string) {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		panic(err)
	}

	tarReader := tar.NewReader(uncompressedStream)

	hardLinks := make(map[string]string)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			panic(err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(filepath.Join(dst, header.Name), header.FileInfo().Mode()); err != nil {
				if !errors.Is(err, os.ErrExist) {
					panic(err)
				}
			}
		case tar.TypeReg:
			outFile, err := os.OpenFile(filepath.Join(dst, header.Name), os.O_RDWR|os.O_CREATE|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				panic(err)
			}
			defer func() {
				err := outFile.Close()
				if err != nil {
					panic(err)
				}
			}()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				panic(err)
			}
		case tar.TypeLink:
			hardLinks[filepath.Join(dst, header.Name)] = filepath.Join(dst, header.Linkname)
		case tar.TypeSymlink:
			err := os.Symlink(header.Linkname, filepath.Join(dst, header.Name))
			if err != nil {
				panic(err)
			}
		default:
			panic(fmt.Sprintf(
				"ExtractTarGz: uknown type: %v in %s",
				header.Typeflag,
				header.Name))
		}
	}
	for k, v := range hardLinks {
		if err := os.Link(v, k); err != nil {
			panic(err)
		}
	}
}

type Manifest struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

func getLayers(manifestPath string) ([]string, error) {
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer manifestFile.Close()
	manifestData, err := ioutil.ReadAll(manifestFile)
	if err != nil {
		return nil, err
	}

	var result []Manifest
	err = json.Unmarshal([]byte(manifestData), &result)
	if err != nil {
		return nil, err
	}
	if len(result) != 1 {
		panic("expected 1 result")
	}
	return result[0].Layers, nil
}

func isVerbose(verbose []bool) bool {
	return len(verbose) > 0
}

func main() {

	args, err := flags.ParseArgs(&opts, os.Args)
	if err != nil {
		panic(err)
	}
	verbose := isVerbose(opts.Verbose)
	progName := "acbrun"
	if len(args) > 0 {
		progName = args[0]
	}
	if len(args) != 5 {
		fmt.Fprintf(os.Stderr, "usage: %s <image.tar.gz> <sha256sum> <container name> <command>\n", progName)
		os.Exit(1)
	}
	image := args[1]
	expectedImageSha256Sum := args[2]
	containerName := args[3]
	command := args[4]

	workingDir, err := os.MkdirTemp("", "")
	if err != nil {
		panic(err)
	}
	if opts.Keep {
		fmt.Printf("keeping temporary working directory: %s\n", workingDir)
	} else {
		defer os.RemoveAll(workingDir)
	}

	r, err := os.Open(image)
	if err != nil {
		panic(err)
	}

	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		panic(err)
	}
	actualSha256Hash := h.Sum(nil)
	actualSha256HashHexString := hex.EncodeToString(actualSha256Hash)

	if actualSha256HashHexString != expectedImageSha256Sum {
		fmt.Fprintf(os.Stderr, "expected sha256 sum %s does not match actual sum of %s: %s\n", expectedImageSha256Sum, image, actualSha256HashHexString)
		os.Exit(1)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "%s sha256sum of %s validation complete\n", image, actualSha256HashHexString)
	}
	r.Seek(0, io.SeekStart)

	defer r.Close()
	ExtractTarGz(r, workingDir)
	layers, err := getLayers(filepath.Join(workingDir, "manifest.json"))
	if err != nil {
		panic(err)
	}
	if len(layers) == 0 {
		panic("no layer data")
	}
	rootFS := filepath.Join(workingDir, "rootfs")
	if err := os.Mkdir(rootFS, 0755); err != nil {
		panic(err)
	}
	for _, layer := range layers {
		if verbose {
			fmt.Fprintf(os.Stderr, "extracting %s\n", layer)
		}
		r, err := os.Open(filepath.Join(workingDir, layer))
		if err != nil {
			panic(err)
		}
		defer r.Close()
		ExtractTarGz(r, rootFS)
	}

	value, err := sjson.Set(string(configJSON), "process.args", []string{"sh", "-c", command})
	if err != nil {
		panic(err)
	}

	// TODO use a JSON lib for this
	// also point to a better location to share
	value = strings.Replace(value, "__path_to_local_dir__", workingDir, 1)

	newConfigFile, err := os.Create(filepath.Join(workingDir, "config.json"))
	if err != nil {
		panic(err)
	}
	defer newConfigFile.Close()
	_, err = newConfigFile.WriteString(value)
	if err != nil {
		panic(err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "running runc\n")
	}
	cmd := exec.Command("runc", "run", containerName)
	cmd.Dir = workingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		panic(err)
	}

}
