## acbrun

A light wrapper around runc, which requires a tar.gz of a rootfs. This tool does not require a network connection.

## Compile

    go build -o acbrun cmd/acbrun/main.go

## Example run

    $ sudo acbrun --verbose --keep sample-images/alpine-3.20.3.tar.gz e99e5d7440c1f31eaa77d84b8d87a8901efde3a1be445161e17bd9a9ea6707c8 containerName "ls -la"
    keeping temporary working directory: /tmp/2120123023
    sample-images/alpine-3.20.3.tar.gz sha256sum of e99e5d7440c1f31eaa77d84b8d87a8901efde3a1be445161e17bd9a9ea6707c8 validation complete
    extracting da9db072f522755cbeb85be2b3f84059b70571b229512f1571d9217b77e1087f.tar.gz
    running runc
    total 68
    drwxr-xr-x   20 root     root          4096 Nov 20 02:39 .
    drwxr-xr-x   20 root     root          4096 Nov 20 02:39 ..
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 bin
    drwxr-xr-x    5 root     root           340 Nov 20 02:39 dev
    drwxr-xr-x   17 root     root          4096 Nov 20 02:39 etc
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 home
    drwxr-xr-x    6 root     root          4096 Nov 20 02:39 lib
    drwx------    3 root     root          4096 Nov 20 02:39 local-dir
    drwxr-xr-x    5 root     root          4096 Nov 20 02:39 media
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 mnt
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 opt
    dr-xr-xr-x  418 root     root             0 Nov 20 02:39 proc
    drwx------    2 root     root          4096 Nov 20 02:39 root
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 run
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 sbin
    drwxr-xr-x    2 root     root          4096 Nov 20 02:39 srv
    dr-xr-xr-x   13 root     root             0 Nov 20 02:39 sys
    drwxr-xr-t    2 root     root          4096 Nov 20 02:39 tmp
    drwxr-xr-x    7 root     root          4096 Nov 20 02:39 usr
    drwxr-xr-x   12 root     root          4096 Nov 20 02:39 var

## Downloading images from a registry

For example, to download alpine, run:

    crane pull alpine:3.20.3 /dev/stdout | gzip -9 > alpine-3.20.3.tar.gz

## Downloading apk packages

First make a directory for outputs:

    mkdir scratch && chmod 0777 scratch

Then download and save files locally

    sudo ./acbrun --bind-local-dir --host-network sample-images/alpine-3.20.3.tar.gz e99e5d7440c1f31eaa77d84b8d87a8901efde3a1be445161e17bd9a9ea6707c8 containerName "echo 'nameserver 8.8.8.8' > /etc/resolv.conf && apk update && cd /local-dir/scratch/ && apk fetch --recursive python3"

Then you can use the apk packages offline:

    sudo ./acbrun --bind-local-dir sample-images/alpine-3.20.3.tar.gz e99e5d7440c1f31eaa77d84b8d87a8901efde3a1be445161e17bd9a9ea6707c8 containerName "rm /etc/apk/repositories && apk add --no-network --no-cache --allow-untrusted /local-dir/scratch/*.apk && python3 --version"
