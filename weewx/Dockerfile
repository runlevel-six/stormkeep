# syntax=docker/dockerfile:1
#
# weewx with the WeatherFlow UDP driver, for a Tempest station.
#
# Build from the repository root:  docker build -f weewx/Dockerfile weewx
# The configuration is not in the image: mount it at /etc/weewx/weewx.conf.
FROM python:3.12-slim-bookworm

# weewx starts each day at local midnight in the zone TZ names. Without the
# zone database glibc falls back to UTC without a word, so install it and
# check that a zone resolves.
RUN apt-get update \
 && apt-get install -y --no-install-recommends tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && test "$(TZ=America/New_York date -d @0 +%Z)" = EST

COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --no-compile -r /tmp/requirements.txt && rm /tmp/requirements.txt

# The driver, at a pinned commit and checked against its hash, so a change
# upstream cannot reach this image without a deliberate edit here. It is GPLv3,
# like weewx itself.
ARG DRIVER_COMMIT=eedfaf7b1ee1d0eb51513966b7a09e7622858791
ADD --checksum=sha256:fbd6bae2b56cc1de753a8778f43e81aface399ec043668e75ab86b7b6066aae6 \
    https://raw.githubusercontent.com/captain-coredump/weatherflow-udp/${DRIVER_COMMIT}/bin/user/weatherflowudp.py \
    /opt/weewx/bin/user/weatherflowudp.py

# See patch-driver.py for what the patch changes and why.
COPY patch-driver.py /tmp/patch-driver.py
RUN python3 /tmp/patch-driver.py /opt/weewx/bin/user/weatherflowudp.py \
 && rm /tmp/patch-driver.py \
 && touch /opt/weewx/bin/user/__init__.py /opt/weewx/bin/user/extensions.py \
 && python3 -m compileall -q /opt/weewx/bin/user \
 && chmod -R a+rX /opt/weewx

# weewx writes only to the database, under /data.
USER 10001:10001
WORKDIR /data
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
ENTRYPOINT ["weewxd"]
CMD ["/etc/weewx/weewx.conf"]
