#!/bin/bash -xe

make bin/proxy

IMAGE_TAG=$(date +%s)
IMAGE_REPO=meln5674/s3-basicauth-proxy
IMAGE="$IMAGE_REPO":"$IMAGE_TAG"
docker build -t "$IMAGE" .
kind load docker-image "$IMAGE"
helm upgrade --install minio minio/minio --set defaultBucket.enabled=true --wait --debug --set DeploymentUpdate.type=Recreate --set resources.requests.memory=128Mi --set resources.limits.memory=512Mi
USERNAME=$(kubectl get secret minio --template '{{ .data.accesskey }}' | base64 -d)
PASSWORD=$(kubectl get secret minio --template '{{ .data.secretkey }}' | base64 -d)
helm delete s3-basicauth-proxy || true
helm upgrade --install s3-basicauth-proxy helm/s3-basicauth-proxy --set image.repository="$IMAGE_REPO" --set image.tag="$IMAGE_TAG" --wait --debug

kubectl port-forward svc/s3-basicauth-proxy 8080:80 &
trap 'kill %1' EXIT

sleep 5

function minio-curl {
    URL="http://localhost:8080/minio:9000/us-east-1/$1"
    shift || true
    curl -v -u "$USERNAME:$PASSWORD" "$@" "$URL"
}

minio-curl
minio-curl bucket/
minio-curl bucket/object --data 'This is a test'
minio-curl bucket/object
minio-curl bucket
minio-curl -X DELETE bucket/object
minio-curl bucket/object
minio-curl bucket
minio-curl -X DELETE bucket
minio-curl bucket
