"""Tests for the forecaster's HTTP layer and batching, with a stand-in model.

    python -m unittest services/forecaster/test_app.py

They need numpy only: the real model is not loaded.
"""

import json
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

import numpy as np

import app

LEVELS = (0.01, 0.05, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99)


class FakeForecast:
    def __init__(self, quantiles):
        self.quantiles = quantiles
        self.quantile_levels = LEVELS

    def quantile(self, level):
        return self.quantiles[..., LEVELS.index(level)]


class FakeModel:
    """Forecasts the last observed value of each row, ± the quantile offset."""

    def __init__(self):
        self.calls = []

    def forecast(self, context, prediction_length):
        self.calls.append((context.shape, prediction_length))
        b = context.shape[0]
        last = np.array([row[~np.isnan(row)][-1] for row in context])
        offsets = np.array(LEVELS) - 0.5
        q = last[:, None, None, None] + offsets[None, None, None, :] * np.ones((b, 1, prediction_length, 1))
        return FakeForecast(q)


class BatchTest(unittest.TestCase):
    def test_groups_by_horizon_and_pads_left(self):
        model = FakeModel()
        values = [np.array([1, 2, 3], np.float32), np.array([5], np.float32), np.array([7, 8], np.float32)]
        out = app.forecast_batch(model, values, [2, 2, 4], (0.1, 0.5, 0.9))
        self.assertEqual(sorted(model.calls), [((1, 2), 4), ((2, 3), 2)])
        self.assertEqual(out[0]["q50"], [3.0, 3.0])
        self.assertEqual(out[1]["q50"], [5.0, 5.0], "a shorter series is padded, not truncated")
        self.assertEqual(len(out[2]["q90"]), 4)
        self.assertAlmostEqual(out[2]["q10"][0], 7.6)

    def test_series_without_values_gets_nulls(self):
        out = app.forecast_batch(FakeModel(), [np.array([np.nan, np.nan], np.float32)], [3], (0.5,))
        self.assertEqual(out[0]["q50"], [None, None, None])

    def test_quantile_not_offered_uses_the_nearest(self):
        out = app.forecast_batch(FakeModel(), [np.array([1], np.float32)], [1], (0.12,))
        self.assertIn("q12", out[0])
        self.assertAlmostEqual(out[0]["q12"][0], 1 + (0.1 - 0.5))


class ParseTest(unittest.TestCase):
    def test_nulls_become_nan(self):
        values, horizons, q = app.parse_request(json.dumps(
            {"series": [{"values": [1, None, 3], "horizon": 5}]}).encode())
        self.assertTrue(np.isnan(values[0][1]))
        self.assertEqual(horizons, [5])
        self.assertEqual(q, app.DEFAULT_QUANTILES)

    def test_rejects(self):
        for body, why in [
            (b"not json", "JSON"),
            (b'{"series": 3}', "series"),
            (b'{"series": [{"values": [1], "horizon": 0}]}', "horizon"),
            (b'{"series": [{"values": [], "horizon": 1}]}', "values"),
            (b'{"series": [{"values": ["a"], "horizon": 1}]}', "numbers"),
            (b'{"quantiles": [2], "series": []}', "quantiles"),
        ]:
            with self.assertRaises(app.BadRequest, msg=why) as ctx:
                app.parse_request(body)
            self.assertIn(why, str(ctx.exception))


class HTTPTest(unittest.TestCase):
    def setUp(self):
        self.service = app.Service("fake-model", FakeModel)
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), app.make_handler(self.service))
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()

    def request(self, path, body=None):
        req = urllib.request.Request(self.url + path, data=body, method="POST" if body is not None else "GET",
                                     headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=5) as r:
                return r.status, json.loads(r.read())
        except urllib.error.HTTPError as e:
            return e.code, json.loads(e.read())

    def test_health_follows_loading(self):
        status, body = self.request("/health")
        self.assertEqual((status, body["status"]), (503, "loading"))
        self.service.load()
        self.assertEqual(self.request("/health"), (200, {"status": "ok", "model": "fake-model"}))

    def test_forecast(self):
        body = json.dumps({"series": [{"values": [1, None, 2], "horizon": 2}]}).encode()
        status, _ = self.request("/v1/forecast", body)
        self.assertEqual(status, 503, "no forecast before the model is loaded")
        self.service.load()
        status, resp = self.request("/v1/forecast", body)
        self.assertEqual(status, 200)
        self.assertEqual(resp["model"], "fake-model")
        self.assertEqual(resp["forecasts"][0]["q50"], [2.0, 2.0])
        status, resp = self.request("/v1/forecast", b'{"series": [{"values": [1], "horizon": 9999}]}')
        self.assertEqual(status, 400)
        self.assertIn("horizon", resp["error"])

    def test_failed_load_is_reported(self):
        def broken():
            raise OSError("no weights in the cache")

        service = app.Service("m", broken)
        service.load()
        self.assertIsNone(service.model)
        self.assertIn("no weights", service.error)


if __name__ == "__main__":
    unittest.main()
