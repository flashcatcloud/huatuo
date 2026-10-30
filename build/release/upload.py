#!/usr/bin/env python3
"""Publish immutable COS objects and verify bytes by downloading them again."""
import hashlib
import os
from pathlib import Path
import re
import sys
import tempfile
from qcloud_cos import CosS3Client


class ImmutableCosClient(CosS3Client):
    def complete_multipart_upload(self, **kwargs):
        # upload_file does not forward headers to its completion request.
        kwargs["Metadata"] = {"x-cos-forbid-overwrite": "true"}
        return super().complete_multipart_upload(**kwargs)


def digest(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def publish(client, bucket, key, path):
    from qcloud_cos import CosServiceError
    try:
        client.head_object(Bucket=bucket, Key=key)
    except CosServiceError as exc:
        if exc.get_status_code() != 404:
            raise
        # COS rejects an existing key, including a concurrent publisher's write.
        print(f"uploading: {key} ({path.stat().st_size} bytes)", flush=True)
        client.upload_file(Bucket=bucket, Key=key, LocalFilePath=str(path),
                           PartSize=4, MAXThread=4, EnableMD5=True,
                           Metadata={"x-cos-forbid-overwrite": "true"})
    print(f"verifying: {key}", flush=True)
    with tempfile.TemporaryDirectory() as directory:
        downloaded = str(Path(directory) / "object")
        client.download_file(Bucket=bucket, Key=key, DestFilePath=downloaded)
        if digest(downloaded) != digest(path):
            raise ValueError(f"immutable object content mismatch: {key}")
    print(f"verified: {key}", flush=True)


def main():
    from qcloud_cos import CosConfig
    directory, version = Path(sys.argv[1]), sys.argv[2]
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", version):
        raise ValueError("invalid release version")
    paths = []
    for arch in ("amd64", "arm64"):
        name = f"huatuo-bamai-{version}-doris-static-linux-{arch}.tar.gz"
        archive, checksum = directory / name, directory / (name + ".sha256")
        if checksum.read_text() != f"{digest(archive)}  {name}\n":
            raise ValueError(f"invalid checksum: {name}")
        paths.extend([archive, checksum])
    for name in ("FC_COS_SECRETID", "FC_COS_SECRETKEY", "FC_COS_BUCKET"):
        if not os.environ.get(name):
            raise ValueError(f"configure release environment secret: {name}")
    config = CosConfig(Timeout=60, Region=os.environ.get("FC_COS_REGION", "ap-nanjing"),
                       SecretId=os.environ["FC_COS_SECRETID"], SecretKey=os.environ["FC_COS_SECRETKEY"])
    client = ImmutableCosClient(config)
    bucket = os.environ["FC_COS_BUCKET"]
    for path in paths:
        publish(client, bucket, f"huatuo/{version}/{path.name}", path)


if __name__ == "__main__":
    main()
