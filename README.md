# Docker File Log Driver
Test project that aims to save Docker log stdout and stderr onto separate files on the host machine, it saves them in plain text. This is my first Docker plugin, and I mangled a lot of `local`, `jsonfile` log driver code to make this one, so use it for testing/insipration for now.

## Installing and using
```
mkdir -p /data/output
make plugin
docker run --log-driver file-log-driver:v1  -d centos bash -c 'for i in {1..100000}; do echo $i; done'
```
After this you should have your output files in `/data/output/<container_id>/`

Have fun!