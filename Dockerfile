FROM python:3.12-slim

# Creation of src directory
WORKDIR /src

# Copy Python libraries requirements and install the required libraries
COPY requirements.txt .
RUN python3.12 -m pip install -r requirements.txt

# Copy source code to working directory (pwd = /src).
COPY /src .
COPY /templates ./templates
COPY /static ./static

# Launch the Python main.py
CMD ["python3.12", "main.py"]
