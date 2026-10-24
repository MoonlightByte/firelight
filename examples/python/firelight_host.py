"""A minimal Firelight host: one world, two turns and one refusal, standard library only.

usage: python3 firelight_host.py [path-to-firelight-apply]
The binary defaults to $FIRELIGHT_APPLY, then ./firelight-apply. See docs/PROTOCOL.md.
"""
import json
import os
import subprocess
import sys

WORLD = """rules { transfer unequips; wear any; }
location "loc:hall" named "Lantern Hall";
character "char:ada" named "Ada" at "loc:hall" {
  stat "defense" = 10;
  resource "hp" = 20 min 0 max 30;
}
item "item:shield:001" named "Bronze Shield" { owner "char:ada"; }
effect "effect:shield" from item "item:shield:001" on worn "item:shield:001" stat "defense" add 2;
"""
ADA = {"kind": "character", "id": "char:ada"}


class EngineFailure(Exception):
    """The process did not return a usable response (exit code 2 or 3)."""


def apply(binary, request):
    """Send one request on stdin and return the decoded response (protocol exit codes 0, 2, 3)."""
    proc = subprocess.run([binary], input=json.dumps(request).encode("ascii"),
                          capture_output=True, timeout=30)
    if proc.returncode == 3:
        # The engine panicked: phase internal, no checkpoint. Store nothing.
        raise EngineFailure("internal error: %s" % json.loads(proc.stdout).get("error"))
    if proc.returncode == 2:
        # The input could not be read or decoded; no world was touched.
        body = json.loads(proc.stdout) if proc.stdout.strip() else {}
        raise EngineFailure("unreadable input: %s" % body.get("error", "no response"))
    if proc.returncode != 0:
        raise EngineFailure("unexpected exit code %d" % proc.returncode)
    return json.loads(proc.stdout)  # exit 0: a complete response; "ok" carries the outcome


def explain_refusal(response):
    """Print a refusal from its typed fields: diagnostics, fault or error (never guess intent)."""
    print("refused in phase %r at revision %d" % (response["phase"], response.get("revision", 0)))
    for d in response.get("diagnostics") or []:
        print("  %s:%d:%d %s expected %s, got %s" % (d["source"], d["line"], d["column"],
                                                    d["code"], d.get("expected"), d.get("actual")))
    if response.get("fault"):
        f = response["fault"]
        print("  fault %s on %s.%s: %s" % (f["code"], f.get("subject"), f.get("field"), f["message"]))
    elif response.get("error") and not response.get("diagnostics"):
        print("  error: %s" % response["error"])


def show(response):
    if response.get("view_error"):  # the transaction stands; a later view failed
        print("  view %s failed: %s" % (response["view_error"]["view"], response["view_error"]["error"]))
    for event in (response.get("receipt") or {}).get("events", []):
        print("  event %s %s" % (event["id"], event["kind"]))
    for status in response.get("status") or []:
        hp = status["resources"]["hp"]
        defense = next(s["effective"] for s in status["stats"] if s["stat"] == "defense")
        print("  %s: hp %d/%d, defense %d" % (status["character"]["name"], hp["current"], hp["maximum"], defense))


def main():
    binary = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("FIRELIGHT_APPLY", "./firelight-apply")
    turns = [
        ("req-1", 'equip "item:shield:001" on "char:ada";\ndamage "char:ada" resource "hp" by 7;'),
        ("req-2", 'heal "char:ada" resource "hp" by 3;'),
        ("req-3", 'damage "char:nobody" resource "hp" by 1;'),
    ]
    checkpoint, revision = None, 0
    for request_id, actions in turns:
        request = {"actions": actions, "actor": ADA, "request": request_id,
                   "expected_revision": revision, "status": ["char:ada"]}
        if checkpoint is None:
            request.update(world=WORLD, world_name="tiny.nql")  # genesis on the first call
        else:
            request["checkpoint"] = checkpoint  # the host owns persistence
        response = apply(binary, request)
        print("%s -> ok=%s phase=%s revision=%d" % (request_id, response["ok"], response["phase"],
                                                   response.get("revision", 0)))
        if not response["ok"]:
            explain_refusal(response)  # nothing changed; keep the stored checkpoint
            continue
        show(response)
        checkpoint, revision = response["checkpoint"], response["revision"]
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except EngineFailure as exc:
        print("engine failure: %s" % exc, file=sys.stderr)
        sys.exit(1)
