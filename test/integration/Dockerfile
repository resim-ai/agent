# Dockerfile to create a customer image with local experience data
# trunk-ignore(hadolint/DL3007)
FROM 909785973729.dkr.ecr.us-east-1.amazonaws.com/rerun-end-to-end-test-experience-build:latest

USER root

# trunk-ignore(hadolint/DL4006)
# trunk-ignore(hadolint/SC3037)
RUN mkdir -p /test_experience_data/experience_1 &&\
  touch /test_experience_data/experience_1/experience_name.txt &&\
  touch /test_experience_data/experience_1/experience_name.base64 &&\
  echo -n "experience_1" > /test_experience_data/experience_1/experience_name.txt &&\
  echo -n "experience_1" | base64 -w 0 > /test_experience_data/experience_1/experience_name.base64

USER 1000
