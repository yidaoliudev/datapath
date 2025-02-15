#!/bin/bash
#set -eux
export GOOS=linux
export CGO_ENABLED=0
export GOARCH=amd64

work_path=$GOPATH"/src/sd-wan-datapath"
path=`pwd`

if [ ! -d $work_path ]; then
    ln -s $path $work_path
fi

echo "path:"$path

rm -rf flow-datapath
mkdir flow-datapath

function checkRet(){
    if [ $? -eq 0 ]; then
        return 0
    else
        exit 100
    fi
}

function makeDatapath() {
    #make pop
    cd $work_path"/service"
    echo "make pop"
    go build -o pop
    checkRet
    cp pop $path"/flow-datapath"

    #tar
    cd $path
    rm -rf flow-datapath.tar.gz
    tar -zcvf flow-datapath.tar.gz flow-datapath
}

makeDatapath

"$@"

