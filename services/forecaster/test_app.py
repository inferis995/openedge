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
    """Forecasts the last observed value of each row (per variate), ± the
    quantile offset; plus the first known covariate's last value, when given."""

    def __init__(self):
        self.calls = []

    def forecast(self, context, prediction_length, known_covariates=None):
        self.calls.append((context.shape, prediction_length, None if known_covariates is None else known_covariates.shape))
        b, v, _ = context.shape
        last = np.array([[row[~np.isnan(row)][-1] for row in series] for series in context])  # (B, V)
        if known_covariates is not None:
            last = last + known_covariates[:, :1, -1]
        offsets = np.array(LEVELS) - 0.5
        q = last[:, :, None, None] + offsets[None, None, None, :] * np.ones((b, v, prediction_length, 1))
        return FakeForecast(q)


def item(values, h, known=None):
    vals = np.array(values, dtype=np.float32)
    joint = vals.ndim == 2
    return app.Item(vals if joint else vals[None, :], h, None if known is None else np.array(known, dtype=np.float32), joint)


class BatchTest(unittest.TestCase):
    def test_groups_by_horizon_and_pads_left(self):
        model = FakeModel()
        items = [item([1, 2, 3], 2), item([5], 2), item([7, 8], 4)]
        out = app.forecast_batch(model, items, (0.1, 0.5, 0.9))
        self.assertEqual(sorted(c[:2] for c in model.calls), [((1, 1, 2), 4), ((2, 1, 3), 2)])
        self.assertEqual(out[0]["q50"], [3.0, 3.0])
        self.assertEqual(out[1]["q50"], [5.0, 5.0], "a shorter series is padded, not truncated")
        self.assertEqual(len(out[2]["q90"]), 4)
        self.assertAlmostEqual(out[2]["q10"][0], 7.6)

    def test_series_without_values_gets_nulls(self):
        out = app.forecast_batch(FakeModel(), [item([np.nan, np.nan], 3)], (0.5,))
        self.assertEqual(out[0]["q50"], [None, None, None])

    def test_quantile_not_offered_uses_the_nearest(self):
        out = app.forecast_batch(FakeModel(), [item([1], 1)], (0.12,))
        self.assertIn("q12", out[0])
        self.assertAlmostEqual(out[0]["q12"][0], 1 + (0.1 - 0.5))

    def test_variates_are_forecast_jointly_and_answered_one_by_one(self):
        model = FakeModel()
        out = app.forecast_batch(model, [item([[1, 2], [10, 20]], 2)], (0.5,))
        self.assertEqual(model.calls[0][0], (1, 2, 2))
        self.assertEqual([v["q50"] for v in out[0]["variates"]], [[2.0, 2.0], [20.0, 20.0]])

    def test_known_covariates_reach_the_model_padded_with_the_history(self):
        model = FakeModel()
        items = [item([1, 2, 3], 1, known=[[0, 0, 0, 5]]), item([4], 1, known=[[0, 7]])]
        out = app.forecast_batch(model, items, (0.5,))
        self.assertEqual(model.calls[0][2], (2, 1, 4), "known covariates span history + horizon of the longest")
        self.assertEqual(out[0]["q50"], [8.0])
        self.assertEqual(out[1]["q50"], [11.0])


class ParseTest(unittest.TestCase):
    def test_nulls_become_nan(self):
        items, q = app.parse_request(json.dumps(
            {"series": [{"values": [1, None, 3], "horizon": 5}]}).encode())
        self.assertTrue(np.isnan(items[0].values[0][1]))
        self.assertEqual(items[0].horizon, 5)
        self.assertFalse(items[0].joint)
        self.assertEqual(q, app.DEFAULT_QUANTILES)

    def test_joint_and_known(self):
        items, _ = app.parse_request(json.dumps({"series": [
            {"values": [[1, 2], [3, None]], "horizon": 1, "known_covariates": [[0, 1, 0]]}]}).encode())
        self.assertTrue(items[0].joint)
        self.assertEqual(items[0].values.shape, (2, 2))
        self.assertEqual(items[0].known.shape, (1, 3))

    def test_rejects(self):
        for body, why in [
            (b"not json", "JSON"),
            (b'{"series": 3}', "series"),
            (b'{"series": [{"values": [1], "horizon": 0}]}', "horizon"),
            (b'{"series": [{"values": [], "horizon": 1}]}', "values"),
            (b'{"series": [{"values": ["a"], "horizon": 1}]}', "numbers"),
            (b'{"quantiles": [2], "series": []}', "quantiles"),
            (b'{"series": [{"values": [[1, 2], [3]], "horizon": 1}]}', "same for every variate"),
            (b'{"series": [{"values": [1, 2], "horizon": 1, "known_covariates": [[0, 1]]}]}', "history + horizon"),
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
