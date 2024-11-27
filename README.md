## acbrun

A light wrapper around runc, which requires a tar.gz of a rootfs. This tool does not require a network connection.

## Compile

    go build -o acbrun cmd/acbrun/main.go

## Example run

    $ sudo acbrun --verbose --keep sample-images/alpine-3.20.3.tar.gz c0d141e28aea48a56c28650de3ceef70767e3d14da5e6d13f4cc68489e97a3e8 "ls -la"
    keeping temporary working directory: /tmp/2120123023
    sample-images/alpine-3.20.3.tar.gz sha256sum of c0d141e28aea48a56c28650de3ceef70767e3d14da5e6d13f4cc68489e97a3e8 validation complete
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

    sudo ./acbrun --bind-local-dir --host-network sample-images/alpine-3.20.3.tar.gz c0d141e28aea48a56c28650de3ceef70767e3d14da5e6d13f4cc68489e97a3e8 "echo 'nameserver 8.8.8.8' > /etc/resolv.conf && apk update && cd /local-dir/scratch/ && apk fetch --recursive python3"

Then you can use the apk packages offline:

    sudo ./acbrun --bind-local-dir sample-images/alpine-3.20.3.tar.gz c0d141e28aea48a56c28650de3ceef70767e3d14da5e6d13f4cc68489e97a3e8 "rm /etc/apk/repositories && apk add --no-network --no-cache --allow-untrusted /local-dir/scratch/*.apk && python3 --version"

## Outputting an Image

use the `--output` flag to export the image after running, for example:

    $ sudo acbrun --output my-output-image.tar.gz sample-images/alpine-3.20.3.tar.gz c0d141e28aea48a56c28650de3ceef70767e3d14da5e6d13f4cc68489e97a3e8 "echo hello world > /root/data"

You can then use the new image:

    $ sudo acbrun my-output-image.tar.gz skip-sha256-validation "ls -la /root/data && cat /root/data"

You can even load them into docker:

    $ sudo docker load < my-output-image.tar.gz 
    eb3578682edc: Loading layer [==================================================>]  3.624MB/3.624MB
    Loaded image ID: sha256:ace010d046a053c4b2cebb168c17296ee48b64e19ecde68f0efc8b64a56585b7
    alex@perch:~/acbrun$ docker run --rm ace010d046a053c4b2cebb168c17296ee48b64e19ecde68f0efc8b64a56585b7 /bin/sh -c 'ls -la /root && cat /root/data'
    docker: permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: Head "http://%2Fvar%2Frun%2Fdocker.sock/_ping": dial unix /var/run/docker.sock: connect: permission denied.
    See 'docker run --help'.
    alex@perch:~/acbrun$ sudo docker run --rm ace010d046a053c4b2cebb168c17296ee48b64e19ecde68f0efc8b64a56585b7 /bin/sh -c 'ls -la /root && cat /root/data'
    total 12
    drwx------    2 root     root          4096 Nov 26 22:19 .
    drwxr-xr-x    1 root     root          4096 Nov 26 22:20 ..
    -rw-r--r--    1 root     root            12 Nov 26 22:19 data
    hello world
