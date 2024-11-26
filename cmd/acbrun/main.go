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
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"
	"github.com/opencontainers/go-digest"
	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tidwall/sjson"
)

//go:embed config.json
var configJSONTemplate string

var opts struct {
	// Slice of bool will append 'true' each time the option
	// is encountered (can be set multiple times, like -vvv)
	Verbose      []bool `short:"v" long:"verbose" description:"Show verbose debug information"`
	Keep         bool   `long:"keep" description:"Keep temporary working directory"`
	HostNetwork  bool   `long:"host-network" description:"Allow host network access"`
	BindLocalDir bool   `long:"bind-local-dir" description:"Bind current working directory to /local-dir"`
	Output       string `long:"output" description:"Output image after execution"`
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

type Manifest struct {
	Config   string   `json:"Config,omitempty"`
	RepoTags []string `json:"RepoTags,omitempty"`
	Layers   []string `json:"Layers,omitempty"`
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

func getSha256String(path string) (string, error) {
	r, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

	actualSha256HashHexString, err := getSha256String(image)
	if err != nil {
		panic(err)
	}

	if actualSha256HashHexString != expectedImageSha256Sum {
		if expectedImageSha256Sum == "skip-sha256-validation" {
			fmt.Fprintf(os.Stderr, "WARNING: continuing due to skip-sha256-validation option (actual value is %s)\n", actualSha256HashHexString)
		} else {
			fmt.Fprintf(os.Stderr, "expected sha256 sum %s does not match actual sum of %s: %s\n", expectedImageSha256Sum, image, actualSha256HashHexString)
			os.Exit(1)
		}
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "%s sha256sum of %s validation complete\n", image, actualSha256HashHexString)
	}
	r, err := os.Open(image)
	if err != nil {
		panic(err)
	}
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

	configJSON := configJSONTemplate

	configJSON, err = sjson.Set(configJSON, "process.args", []string{"sh", "-c", command})
	if err != nil {
		panic(err)
	}

	if !opts.HostNetwork {
		configJSON, err = sjson.Set(configJSON, "linux.namespaces.-1", map[string]string{"type": "network"})
		if err != nil {
			panic(err)
		}
	}

	if opts.BindLocalDir {
		actualWorkingDir, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		configJSON, err = sjson.Set(configJSON, "mounts.-1", map[string]interface{}{
			"destination": "/local-dir",
			"type":        "bind",
			"source":      actualWorkingDir,
			"options": []string{
				"rbind",
				"rprivate",
			},
		})
		if err != nil {
			panic(err)
		}

	}

	configJSON, err = sjson.Set(configJSON, "process.args", []string{"sh", "-c", command})
	if err != nil {
		panic(err)
	}

	newConfigFile, err := os.Create(filepath.Join(workingDir, "config.json"))
	if err != nil {
		panic(err)
	}
	defer newConfigFile.Close()
	_, err = newConfigFile.WriteString(configJSON)
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

	if opts.Output == "" {
		return
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "outputing image to %s\n", opts.Output)
	}

	outputDir, err := os.MkdirTemp("", "")
	if err != nil {
		panic(err)
	}
	fmt.Printf("output dir: %s\n", outputDir)
	defer os.RemoveAll(outputDir)

	rootFSPath := filepath.Join(outputDir, "rootfs.tar.gz")
	out, err := os.Create(rootFSPath)
	if err != nil {
		panic(err)
	}
	defer out.Close()

	err = CreateTarGz(rootFS, out)
	if err != nil {
		panic(err)
	}

	outputRootFSTarGzSha256, err := getSha256String(rootFSPath)
	if err != nil {
		panic(err)
	}
	rootFSName := fmt.Sprintf("%s.tar.gz", outputRootFSTarGzSha256)
	err = os.Rename(rootFSPath, filepath.Join(outputDir, rootFSName))
	if err != nil {
		panic(err)
	}

	imageConfig := imagespec.Image{
		Platform: imagespec.Platform{
			Architecture: "amd64", // TODO
			OS:           "linux",
		},
		Config: imagespec.ImageConfig{
			Env: []string{
				"PATH=/bin:/usr/bin", // TODO
			},
		},
		RootFS: imagespec.RootFS{
			Type: "layers",
			DiffIDs: []digest.Digest{
				digest.Digest(fmt.Sprintf("sha256:%s", outputRootFSTarGzSha256)),
			},
		},
	}
	imageConfigJSON, err := json.Marshal(imageConfig)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", imageConfigJSON)

	h := sha256.New()
	h.Write(imageConfigJSON)
	imageConfigJSONSha256 := hex.EncodeToString(h.Sum(nil))

	imageConfigName := fmt.Sprintf("sha256:%s", imageConfigJSONSha256)
	imageConfigJSONFile, err := os.Create(filepath.Join(outputDir, imageConfigName))
	if err != nil {
		panic(err)
	}
	defer imageConfigJSONFile.Close()
	_, err = imageConfigJSONFile.Write(imageConfigJSON)
	if err != nil {
		panic(err)
	}

	imageManifest := Manifest{
		Config: imageConfigName,
		Layers: []string{rootFSName},
	}
	imageManifestJson, err := json.Marshal([]Manifest{imageManifest})
	if err != nil {
		panic(err)
	}

	imageManifestJsonFile, err := os.Create(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		panic(err)
	}
	defer imageManifestJsonFile.Close()
	_, err = imageManifestJsonFile.Write(imageManifestJson)
	if err != nil {
		panic(err)
	}

	outputImage, err := os.Create(opts.Output)
	if err != nil {
		panic(err)
	}
	defer outputImage.Close()

	err = CreateTarGz(outputDir, outputImage)
	if err != nil {
		panic(err)
	}

}
