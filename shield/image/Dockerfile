#
# Copyright 2020 IBM Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# FROM registry.access.redhat.com/ubi7/ubi:latest
FROM registry.access.redhat.com/ubi7/ubi-minimal:7.8

# RUN yum install -y tar

RUN mkdir -p /ishield-app && mkdir -p /ishield-app/public
COPY ./ishield-server /ishield-app

RUN chgrp -R 0 /ishield-app && chmod -R g=u /ishield-app

WORKDIR /ishield-app

ENTRYPOINT ["/ishield-app/ishield-server"]
