import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
from qcloud_cos import CosServiceError


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


upload = load("upload")
verify = load("verify")


class UploadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "artifact"
        self.path.write_bytes(b"release")
        self.client = Mock()
        self.client.download_file.side_effect = lambda **kw: Path(kw["DestFilePath"]).write_bytes(b"release")

    def run_publish(self):
        upload.publish(self.client, "bucket", "huatuo/v1.0.0/file", self.path)

    def test_identical_existing_is_not_overwritten(self):
        self.run_publish()
        self.client.put_object.assert_not_called()

    def test_existing_conflict_fails(self):
        self.client.download_file.side_effect = lambda **kw: Path(kw["DestFilePath"]).write_bytes(b"different")
        with self.assertRaises(ValueError):
            self.run_publish()
        self.client.put_object.assert_not_called()

    def test_missing_uploads_with_overwrite_protection(self):
        self.client.head_object.side_effect = CosServiceError("HEAD", {}, 404)
        self.run_publish()
        self.assertEqual(self.client.put_object.call_args.kwargs["Metadata"], {"x-cos-forbid-overwrite": "true"})

    def test_sdk_header_mapping(self):
        from qcloud_cos.cos_comm import mapped
        self.assertEqual(mapped({"Metadata": {"x-cos-forbid-overwrite": "true"}}),
                         {"x-cos-forbid-overwrite": "true"})

    def test_access_denied_fails_without_upload(self):
        self.client.head_object.side_effect = CosServiceError("HEAD", {}, 403)
        with self.assertRaises(CosServiceError):
            self.run_publish()
        self.client.put_object.assert_not_called()

    def test_upload_error_propagates(self):
        self.client.head_object.side_effect = CosServiceError("HEAD", {}, 404)
        self.client.put_object.side_effect = RuntimeError("network failure")
        with self.assertRaises(RuntimeError):
            self.run_publish()
        self.client.download_file.assert_not_called()

    def test_uploaded_bytes_must_match(self):
        self.client.head_object.side_effect = CosServiceError("HEAD", {}, 404)
        self.client.download_file.side_effect = lambda **kw: Path(kw["DestFilePath"]).write_bytes(b"truncated")
        with self.assertRaises(ValueError):
            self.run_publish()


class VerifyTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for name in ["bin/huatuo-bamai", "bin/asprof", "py-spy", "lib/libasyncProfiler.so"]:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"\x7fELF\x02\x01" + bytes(12) + b"\x3e\x00")
        for name in ["conf/huatuo-bamai.conf", "conf/huatuo-apiserver.conf", "LICENSE"]:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture")
        (self.root / "build.json").write_text('{"architecture":"amd64"}')
        (self.root / "bpf").mkdir()
        (self.root / "bpf/test.o").write_bytes(b"\x7fELF\x02\x01" + bytes(12) + b"\xf7\x00")

    @patch.object(verify.subprocess, "run")
    @patch.object(verify.subprocess, "check_output", return_value="")
    def test_valid_shape(self, check, run):
        verify.verify(self.root, "amd64")
        self.assertEqual(run.call_count, 3)

    def test_wrong_architecture(self):
        with self.assertRaises(ValueError):
            verify.verify(self.root, "arm64")

    def test_missing_sampler(self):
        (self.root / "py-spy").unlink()
        with self.assertRaises(ValueError):
            verify.verify(self.root, "amd64")

    @patch.object(verify.subprocess, "check_output", return_value="INTERP")
    def test_dynamic_huatuo_rejected(self, check):
        with self.assertRaises(ValueError):
            verify.verify(self.root, "amd64")

    def test_metadata_file_rejected(self):
        (self.root / "._LICENSE").write_text("metadata")
        with self.assertRaises(ValueError):
            verify.verify(self.root, "amd64")


if __name__ == "__main__":
    unittest.main()
