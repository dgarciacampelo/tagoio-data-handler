FROM python:3.11-bullseye

# Creation of src directory
WORKDIR /src

# Copy Python libraries requirements and install the required libraries
COPY requirements.txt .
RUN python3.11 -m pip install -r requirements.txt

# Copy source code to working directory (pwd = /src).
COPY /src .

# Launch the Python main.py
CMD ["python3.11", "main.py"]
