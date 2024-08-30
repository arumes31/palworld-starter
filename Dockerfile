# Use an official Python runtime as a parent image
FROM python:3.9-slim

# Set the working directory
WORKDIR /app

# Copy the current directory contents into the container at /app
COPY . /app

# Install any needed packages specified in requirements.txt
RUN pip install --no-cache-dir -r requirements.txt
RUN pip install Werkzeug==2.0.3
RUN pip install requests==2.31.0
RUN pip install --upgrade docker
RUN pip install --upgrade requests
#RUN pip install --upgrade Werkzeug
RUN pip install gunicorn
RUN pip install apscheduler

# Make port 5000 available to the world outside this container
EXPOSE 5000

# Command to run the application using Gunicorn
CMD ["gunicorn", "-w", "1", "-b", "0.0.0.0:5000", "app:app"]
