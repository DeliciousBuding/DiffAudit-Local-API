FROM python:3.11-slim

WORKDIR /runner

COPY runners/shared/src /runner/src
COPY runners/shared/requirements-pia.txt /runner/requirements.txt
COPY runners/pia-runner/run.py /runner/run.py

RUN pip install --no-cache-dir -r /runner/requirements.txt

ENTRYPOINT ["python", "/runner/run.py"]
