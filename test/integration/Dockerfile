# Dockerfile to create a customer image with local experience data
# ARG TEST_BUILD_TAG

FROM 909785973729.dkr.ecr.us-east-1.amazonaws.com/rerun-end-to-end-test-experience-build:latest

USER root

RUN mkdir -p /test_experience_data/experience_location_1 &&\
  mkdir -p /test_experience_data/experience_location_2 &&\
  mkdir -p /test_experience_data/experience_location_1/ExperienceName &&\
  touch /test_experience_data/experience_location_1/ExperienceName/experience_name.txt &&\
  touch /test_experience_data/experience_location_2/experience_name.base64 &&\
  echo -n "experience_1" > /test_experience_data/experience_location_1/ExperienceName/experience_name.txt &&\
  echo -n "experience_1" | base64 -w 0 > /test_experience_data/experience_location_2/experience_name.base64

USER 1000
