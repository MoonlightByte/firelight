"""Read-only classifier for consecutive (possibly multiline) CLI JSON objects.

Reads stdin, writes summaries to stdout. Exit 2 denotes a consumer protocol
problem. It never sends actions or interprets Expected/Actual as numeric data.
"""
import json
import sys


class ProtocolProblem(ValueError):
    pass


def require(ok, message):
    if not ok:
        raise ProtocolProblem(message)


def keys(value, names):
    require(type(value) is dict and set(value) == set(names), "missing or unexpected record fields")


def strings(value, names):
    require(all(type(value[k]) is str for k in names), "expected string fields")


def classify(record):
    require(type(record) is dict, "expected JSON object")
    if "runtime_diagnostic" in record:
        keys(record, ("error", "runtime_diagnostic"))
        strings(record, ("error",))
        d = record["runtime_diagnostic"]
        keys(d, ("version", "phase", "execution", "fault", "ability"))
        require(type(d["version"]) is int and d["version"] == 1, "unsupported runtime diagnostic version")
        require(d["phase"] == "execute", "unsupported diagnostic phase")
        e = d["execution"]
        keys(e, ("attempt_committed", "detail_scope"))
        require(e["attempt_committed"] is False and e["detail_scope"] == "evaluation_not_snapshot", "invalid evaluation markers")
        f, a = d["fault"], d["ability"]
        if f is not None:
            names = ("code", "subject", "field", "expected", "actual", "message")
            keys(f, names)
            strings(f, names)
        if a is not None:
            keys(a, ("definition", "step", "operand", "parameter"))
            strings(a, ("definition", "operand", "parameter"))
            require(type(a["step"]) is int, "expected integer body step")
        return {"category": "runtime_rejection", "error": record["error"], "execution": e, "fault": f, "ability": a}
    if "error" in record:
        keys(record, ("error",))
        strings(record, ("error",))
        return {"category": "legacy_error", "error": record["error"], "structured_detail": None}
    if "diagnostics" in record:
        keys(record, ("diagnostics",))
        ds = record["diagnostics"]
        require(type(ds) is list and len(ds) > 0, "expected compiler diagnostic array")
        for d in ds:
            names = {"source", "line", "column", "code", "expected", "actual", "message"}
            require(type(d) is dict and names <= set(d) <= names | {"suggestion"}, "invalid compiler diagnostic fields")
            strings(d, set(d) - {"line", "column"})
            require(all(type(d[k]) is int and d[k] >= 1 for k in ("line", "column")), "invalid compiler position")
        return {"category": "compiler_diagnostics", "diagnostics": ds}
    require("revision" in record and type(record["revision"]) is int and record["revision"] >= 0, "unrecognized CLI record")
    payloads = set(record) - {"revision"}
    require(bool(payloads) and payloads <= {"result", "world", "status", "transfer"}, "unrecognized success payload")
    require(all(type(record[k]) is dict for k in payloads), "invalid success payload type")
    return {"category": "success", "revision": record["revision"]}


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON key")
        result[key] = value
    return result


def invalid_constant(_):
    raise ProtocolProblem("invalid JSON constant")


def summaries(text):
    decoder = json.JSONDecoder(object_pairs_hook=unique_object, parse_constant=invalid_constant)
    while text.strip():
        record, end = decoder.raw_decode(text.lstrip())
        yield classify(record)
        text = text.lstrip()[end:]


def main():
    try:
        for summary in summaries(sys.stdin.read()):
            print(json.dumps(summary))
    except (ValueError, RecursionError) as error:
        # Decoder limits are protocol failures too. ASCII escaping also permits
        # decoded JSON strings containing lone surrogates to be rendered safely.
        print(json.dumps({"category": "consumer_protocol_problem", "message": str(error)}))
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
