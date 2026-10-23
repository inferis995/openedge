# OpenEdge forecaster: Timestral on CPU, behind a small HTTP API for core-api.
# See docs/FORECASTING.md. Optional: compose profile "forecast".

# Build stage: resolve and install the Python packages into a virtualenv.
FROM python:3.13-slim AS builder

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

# torch 2.14.1 is the version the timestral release was locked against.
ARG TORCH_VERSION=2.14.1
# Inference code for the Timestral models (Apache-2.0), pinned to the release
# commit. The weights are not in the image: they are downloaded on first start
# into the /models volume (see docs/FORECASTING.md).
ARG TIMESTRAL_REF=a534bdbd244d64f3d9d743f46c7bf6fc330ed4ff

RUN python -m venv /opt/venv
ENV PATH=/opt/venv/bin:$PATH
COPY services/forecaster/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu "torch==${TORCH_VERSION}" \
    && pip install --no-cache-dir -r /tmp/requirements.txt \
    && pip install --no-cache-dir --no-deps "timestral @ git+https://github.com/mistralai/timestral.git@${TIMESTRAL_REF}" \
    && python -c "import torch, timestral; print('torch', torch.__version__)"

# Runtime stage.
FROM python:3.13-slim

# OS security patches published since the base tag was built (same reason as
# the Alpine images' `apk upgrade`).
RUN apt-get update \
    && apt-get upgrade -y \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --system --uid 10001 --home-dir /app forecaster \
    && mkdir -p /app /models \
    && chown forecaster /models

COPY --from=builder /opt/venv /opt/venv
COPY services/forecaster/app.py /app/app.py

ENV PATH=/opt/venv/bin:$PATH \
    PYTHONUNBUFFERED=1 \
    HF_HOME=/models \
    HF_HUB_DISABLE_TELEMETRY=1 \
    TIMESTRAL_MODEL=mistralai/Timestral-28M \
    PORT=8000

USER forecaster
WORKDIR /app
EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=120s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8000/health', timeout=4).status == 200 else 1)"

CMD ["python", "app.py"]
