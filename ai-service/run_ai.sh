#!/bin/bash
cd /home/none/clawpro/ai-service
source venv/bin/activate
export PYTHONPATH=$(pwd)
nohup python -m src.main > /tmp/ai.log 2>&1 &
echo "AI Service started with PID $!"
