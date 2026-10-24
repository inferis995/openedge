"""OpenEdge forecaster: serves a time-series model to core-api.

core-api (internal/forecast/remote.go) sends a batch of series and gets back
three quantiles per step:

    POST /v1/forecast
    {"quantiles": [0.1, 0.5, 0.9],
     "series": [{"values": [1.2, null, 1.4, ...], "horizon": 24}, ...]}
    ->
    {"model": "mistralai/Timestral-28M",
     "forecasts": [{"q10": [...], "q50": [...], "q90": [...]}, ...]}

A series may carry "known_covariates": rows known over history and horizon
(time of day, weekend, shift on, maintenance), and "values" may be a list of
rows — the variates of one machine, forecast jointly; the answer is then
{"variates": [{"q10": …}, …]}, one per row.

    GET /health  -> 200 once the model is loaded, 503 before.

null is a missing value, both ways. The model runs locally on CPU: no data
leaves the server. Only the Python standard library is used for HTTP, so the
image carries nothing but the model's own dependencies.

Environment:
    TIMESTRAL_MODEL   Hub repo id or local directory (mistralai/Timestral-28M)
    TIMESTRAL_REVISION  optional pin of the Hub revision
    TORCH_THREADS     CPU threads for inference (default: all)
    PORT              listening port (8000)
    HF_HOME           where the weights are cached (/models in the image)
    HF_HUB_OFFLINE=1  never go to the network: the weights must be cached
"""

from __future__ import annotations

import json
import logging
import math
import os
import threading
import time
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np

log = logging.getLogger("forecaster")

MAX_BODY = 64 << 20
MAX_SERIES = 512
MAX_LENGTH = 32768
MAX_HORIZON = 1024
DEFAULT_QUANTILES = (0.1, 0.5, 0.9)


class BadRequest(ValueError):
    pass


def _row(v, what: str) -> np.ndarray:
    try:
        return np.array([math.nan if x is None else float(x) for x in v], dtype=np.float32)
    except (TypeError, ValueError) as e:
        raise BadRequest(f"{what}: values must be numbers or null") from e


class Item:
    """One series of a request: V variates over T steps, forecast H steps ahead,
    with C known-future covariate rows over T+H steps (C may be 0)."""

    def __init__(self, values: np.ndarray, horizon: int, known: np.ndarray | None, joint: bool):
        self.values = values      # (V, T)
        self.horizon = horizon
        self.known = known        # (C, T+H) or None
        self.joint = joint        # answered with one forecast per variate

    @property
    def key(self):
        return (self.horizon, self.values.shape[0], 0 if self.known is None else self.known.shape[0])


def parse_request(body: bytes) -> tuple[list[Item], tuple[float, ...]]:
    """Validate a request; return its items (NaN for null) and the quantiles."""
    try:
        req = json.loads(body)
    except ValueError as e:
        raise BadRequest(f"body is not JSON: {e}") from e
    if not isinstance(req, dict) or not isinstance(req.get("series"), list):
        raise BadRequest("body must be an object with a 'series' list")
    series = req["series"]
    if len(series) > MAX_SERIES:
        raise BadRequest(f"at most {MAX_SERIES} series per request")
    quantiles = tuple(req.get("quantiles") or DEFAULT_QUANTILES)
    if not all(isinstance(q, (int, float)) and 0 < q < 1 for q in quantiles):
        raise BadRequest("quantiles must be numbers between 0 and 1")
    items = []
    for i, s in enumerate(series):
        if not isinstance(s, dict) or not isinstance(s.get("values"), list):
            raise BadRequest(f"series {i}: needs a 'values' list")
        h = s.get("horizon")
        if not isinstance(h, int) or not 1 <= h <= MAX_HORIZON:
            raise BadRequest(f"series {i}: horizon must be an integer between 1 and {MAX_HORIZON}")
        v = s["values"]
        joint = len(v) > 0 and isinstance(v[0], list)
        rows = [_row(r, f"series {i}") for r in v] if joint else [_row(v, f"series {i}")]
        t = len(rows[0])
        if not 1 <= t <= MAX_LENGTH or any(len(r) != t for r in rows):
            raise BadRequest(f"series {i}: between 1 and {MAX_LENGTH} values, the same for every variate")
        known = None
        if s.get("known_covariates"):
            k = s["known_covariates"]
            if not isinstance(k, list) or not all(isinstance(r, list) for r in k):
                raise BadRequest(f"series {i}: known_covariates must be a list of rows")
            known = np.stack([_row(r, f"series {i} known_covariates") for r in k])
            if known.shape[1] != t + h:
                raise BadRequest(f"series {i}: each known_covariates row needs {t + h} values (history + horizon)")
        items.append(Item(np.stack(rows), h, known, joint))
    return items, quantiles


def to_numpy(x) -> np.ndarray:
    if hasattr(x, "detach"):
        x = x.detach().cpu().numpy()
    return np.asarray(x, dtype=np.float64)


def nearest_level(levels: tuple[float, ...], q: float) -> float:
    return min(levels, key=lambda lv: abs(lv - q))


def _bands(empty: bool, quantiles, h: int, arrs) -> dict:
    out = {}
    for q in quantiles:
        vals = arrs[q][:h]
        out[f"q{round(q * 100)}"] = [None if empty or not math.isfinite(x) else float(x) for x in vals]
    return out


def forecast_batch(model, items: list[Item], quantiles: tuple[float, ...]) -> list[dict]:
    """Forecast every item, calling the model once per (horizon, variates,
    covariates) group. Items of one call must have the same length: shorter
    ones are padded on the left with NaN, which the model reads as missing —
    their covariates too."""
    out: list[dict | None] = [None] * len(items)
    groups: dict[tuple, list[int]] = {}
    for i, it in enumerate(items):
        groups.setdefault(it.key, []).append(i)
    for (h, nv, nc), idx in groups.items():
        width = max(items[i].values.shape[1] for i in idx)
        ctx = np.full((len(idx), nv, width), np.nan, dtype=np.float32)
        known = np.full((len(idx), nc, width + h), np.nan, dtype=np.float32) if nc else None
        for row, i in enumerate(idx):
            it = items[i]
            t = it.values.shape[1]
            ctx[row, :, width - t:] = it.values
            if known is not None:
                known[row, :, width - t:] = it.known
        # A variate with no value at all: nothing to forecast from. The model
        # is given zeros and core-api sees nulls.
        empty = np.isnan(ctx).all(axis=2)  # (B, V)
        ctx[empty] = 0.0
        kwargs = {"prediction_length": h}
        if known is not None:
            kwargs["known_covariates"] = known
        fc = model.forecast(ctx, **kwargs)
        levels = tuple(fc.quantile_levels)
        bands = {q: to_numpy(fc.quantile(nearest_level(levels, q))).reshape(len(idx), nv, -1) for q in quantiles}
        for row, i in enumerate(idx):
            per = [_bands(bool(empty[row, v]), quantiles, h, {q: bands[q][row, v] for q in quantiles})
                   for v in range(nv)]
            out[i] = {"variates": per} if items[i].joint else per[0]
    return out  # type: ignore[return-value]


class Service:
    """The loaded model and what the HTTP handler needs from it."""

    def __init__(self, name: str, loader):
        self.name = name
        self._loader = loader
        self.model = None
        self.error: str | None = None
        # One inference at a time: the model already uses every CPU thread,
        # and two batches side by side would only fight over them.
        self.lock = threading.Lock()

    def load(self) -> None:
        start = time.monotonic()
        try:
            self.model = self._loader()
            log.info("model %s loaded in %.1fs", self.name, time.monotonic() - start)
        except Exception as e:  # noqa: BLE001 — reported on /health, retried by restart
            self.error = f"{type(e).__name__}: {e}"
            log.exception("could not load model %s", self.name)

    def forecast(self, body: bytes) -> dict:
        items, quantiles = parse_request(body)
        with self.lock:
            forecasts = forecast_batch(self.model, items, quantiles) if items else []
        return {"model": self.name, "forecasts": forecasts}


def make_handler(service: Service):
    class Handler(BaseHTTPRequestHandler):
        server_version = "openedge-forecaster"

        def _send(self, status: int, payload: dict) -> None:
            data = json.dumps(payload, allow_nan=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):  # noqa: N802
            if self.path != "/health":
                self._send(HTTPStatus.NOT_FOUND, {"error": "not found"})
            elif service.model is None:
                self._send(HTTPStatus.SERVICE_UNAVAILABLE,
                           {"status": "loading" if service.error is None else "failed",
                            "model": service.name, "error": service.error})
            else:
                self._send(HTTPStatus.OK, {"status": "ok", "model": service.name})

        def do_POST(self):  # noqa: N802
            if self.path != "/v1/forecast":
                self._send(HTTPStatus.NOT_FOUND, {"error": "not found"})
                return
            if service.model is None:
                self._send(HTTPStatus.SERVICE_UNAVAILABLE, {"error": "model not loaded", "model": service.name})
                return
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0 or length > MAX_BODY:
                self._send(HTTPStatus.REQUEST_ENTITY_TOO_LARGE if length > MAX_BODY else HTTPStatus.BAD_REQUEST,
                           {"error": f"body must be between 1 byte and {MAX_BODY} bytes"})
                return
            body = self.rfile.read(length)
            start = time.monotonic()
            try:
                resp = service.forecast(body)
            except BadRequest as e:
                self._send(HTTPStatus.BAD_REQUEST, {"error": str(e)})
                return
            except Exception as e:  # noqa: BLE001
                log.exception("forecast failed")
                self._send(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": f"{type(e).__name__}: {e}"})
                return
            log.info("%d series in %.2fs", len(resp["forecasts"]), time.monotonic() - start)
            self._send(HTTPStatus.OK, resp)

        def log_message(self, fmt, *args):  # quiet access log; errors are logged above
            log.debug(fmt, *args)

    return Handler


def timestral_loader(name: str, revision: str | None):
    def load():
        import torch
        from timestral import TimestralForecaster

        threads = int(os.environ.get("TORCH_THREADS") or 0)
        if threads > 0:
            torch.set_num_threads(threads)
        return TimestralForecaster.from_pretrained(name, device="cpu", revision=revision or None)

    return load


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    name = os.environ.get("TIMESTRAL_MODEL", "mistralai/Timestral-28M")
    service = Service(name, timestral_loader(name, os.environ.get("TIMESTRAL_REVISION")))
    # Load in the background so /health answers (503) while the weights are
    # being read or downloaded.
    threading.Thread(target=service.load, daemon=True).start()
    port = int(os.environ.get("PORT", "8000"))
    server = ThreadingHTTPServer(("0.0.0.0", port), make_handler(service))
    log.info("listening on :%d (model %s)", port, name)
    server.serve_forever()


if __name__ == "__main__":
    main()
