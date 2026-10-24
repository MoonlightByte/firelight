"""Synthetic decoder controls; actual binary records are tested by check-cli.py."""
import copy
import json
import pathlib
import subprocess
import sys
import unittest
import read_diagnostics as reader


def fixture():
    return {"error": "arbitrary sentence", "runtime_diagnostic": {
        "version": 1, "phase": "execute", "execution": {"attempt_committed": False, "detail_scope": "evaluation_not_snapshot"},
        "fault": {"code": "E_FUTURE", "subject": 'quote"\\{}é', "field": "", "expected": "a\nb", "actual": "current=-9223372036854775808", "message": "text"},
        "ability": {"definition": "ability:é", "step": 0, "operand": "payer", "parameter": ""}}}


class ConsumerTests(unittest.TestCase):
    def test_process_decoder_limits_and_string_rendering(self):
        source = json.dumps(fixture())
        surrogate = fixture()
        surrogate["runtime_diagnostic"]["fault"]["subject"] = "\ud800"
        cases = [
            (source.replace('"version": 1', '"version": ' + '9' * 4301), 2, "consumer_protocol_problem"),
            (source.replace('"version": 1', '"version": ' + '[' * 1100 + '0' + ']' * 1100), 2, "consumer_protocol_problem"),
            ('{"error":"\\ud800"}', 0, "legacy_error"),
            (json.dumps(surrogate), 0, "runtime_rejection"),
        ]
        for text, exit_code, category in cases:
            with self.subTest(category=category, length=len(text)):
                result = subprocess.run([sys.executable, str(pathlib.Path(reader.__file__))], input=text, text=True, capture_output=True)
                self.assertEqual(result.returncode, exit_code, result.stderr)
                self.assertEqual(result.stderr, "")
                summary = json.loads(result.stdout)
                self.assertEqual(summary["category"], category)
                if category == "legacy_error":
                    self.assertEqual(summary["error"], "\ud800")
                if category == "runtime_rejection":
                    self.assertEqual(summary["fault"]["subject"], "\ud800")

    def test_message_independence_and_exact_data(self):
        original = fixture()
        changed = copy.deepcopy(original)
        changed["error"] = "A wholly different story says SUCCESS"
        changed["runtime_diagnostic"]["fault"]["message"] = "Different localized sentence"
        a, b = reader.classify(original), reader.classify(changed)
        self.assertEqual(a["category"], b["category"])
        for key in ("code", "subject", "field", "expected", "actual"):
            self.assertEqual(a["fault"][key], b["fault"][key])
        self.assertEqual(b["ability"]["step"], 0)
        changed["runtime_diagnostic"]["ability"]["step"] = -1
        self.assertEqual(reader.classify(changed)["ability"]["step"], -1)
        self.assertEqual(original, fixture())

    def test_null_empty_and_legacy(self):
        value = fixture()
        value["runtime_diagnostic"]["fault"] = None
        value["runtime_diagnostic"]["ability"] = None
        self.assertIsNone(reader.classify(value)["fault"])
        self.assertIsNone(reader.classify(value)["ability"])
        value["runtime_diagnostic"]["fault"] = dict.fromkeys(("code", "subject", "field", "expected", "actual", "message"), "")
        self.assertEqual(reader.classify(value)["fault"]["code"], "")
        self.assertEqual(reader.classify({"error": "E_INSUFFICIENT_RESOURCE in prose"})["category"], "legacy_error")

    def test_claimed_protocol_rejections(self):
        edits = [
            lambda x: x.update(runtime_diagnostic=None),
            lambda x: x["runtime_diagnostic"].update(version=2),
            lambda x: x["runtime_diagnostic"].update(version=True),
            lambda x: x["runtime_diagnostic"].update(version=1.0),
            lambda x: x["runtime_diagnostic"].update(phase="write"),
            lambda x: x["runtime_diagnostic"].update(current=1),
            lambda x: x["runtime_diagnostic"]["execution"].update(attempt_committed=True),
            lambda x: x["runtime_diagnostic"]["execution"].update(attempt_committed=0),
            lambda x: x["runtime_diagnostic"]["execution"].update(detail_scope="snapshot"),
            lambda x: x["runtime_diagnostic"]["fault"].pop("actual"),
            lambda x: x["runtime_diagnostic"]["fault"].update(actual=1),
            lambda x: x["runtime_diagnostic"]["ability"].update(step=False),
            lambda x: x["runtime_diagnostic"]["ability"].update(step="0"),
            lambda x: x["runtime_diagnostic"]["ability"].pop("parameter"),
            lambda x: x["runtime_diagnostic"]["ability"].update(parameter=None),
            lambda x: x.update(result={}),
        ]
        for index, change in enumerate(edits):
            with self.subTest(index=index):
                value = fixture(); change(value)
                with self.assertRaises(reader.ProtocolProblem):
                    reader.classify(value)
        for value in ([], None, {}, {"revision": True, "world": {}}, {"revision": 1}, {"revision": 1, "result": None}, {"diagnostics": []}):
            with self.subTest(value=value), self.assertRaises(reader.ProtocolProblem):
                reader.classify(value)

    def test_multiline_records_and_trailing_malformed_input(self):
        compiler = {"diagnostics": [{"source": "a", "line": 1, "column": 2, "code": "E_PARSE", "expected": "integer", "actual": ";", "message": "bad"}]}
        stream = " \n\t" + json.dumps(fixture(), indent=2) + "\r\n" + json.dumps(compiler, indent=2) + " \n" + json.dumps({"revision": 1, "result": {}})
        self.assertEqual([x["category"] for x in reader.summaries(stream)], ["runtime_rejection", "compiler_diagnostics", "success"])
        with self.assertRaises(json.JSONDecodeError):
            list(reader.summaries(stream + "\n{"))
        with self.assertRaises(reader.ProtocolProblem):
            list(reader.summaries('{"error":"one","error":"two"}'))
        with self.assertRaises(reader.ProtocolProblem):
            list(reader.summaries('{"revision":NaN,"world":{}}'))


if __name__ == "__main__":
    unittest.main()
