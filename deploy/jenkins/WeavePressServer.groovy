pipeline {
  agent any

  options {
    timestamps()
    disableConcurrentBuilds()
    skipDefaultCheckout(true)
    buildDiscarder(logRotator(numToKeepStr: '20'))
  }

  parameters {
    string(name: 'BRANCH', defaultValue: 'master', description: '生产构建只允许 master。')
    string(name: 'APP_SHA', defaultValue: '', description: '必须是 master 上完整的 40 位小写 commit SHA。')
    booleanParam(name: 'PUSH_ACR', defaultValue: false, description: '将本次精确 SHA 的三个镜像推送到 ACR。')
    booleanParam(name: 'DEPLOY', defaultValue: false, description: '推送镜像后通过受限 SSH 发布 Server。')
    string(name: 'TRIGGER_REPO', defaultValue: 'manual', description: '固定审计来源。')
  }

  environment {
    NAS_REPO = 'ssh://chenhua@192.168.31.240/volume1/docker/weavepress-git/WeavePress.git'
    NAS_GIT_SSH_COMMAND = 'ssh -i /var/jenkins_home/.ssh/nas_classmate_git_ed25519 -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/var/jenkins_home/.ssh/known_hosts'
    ACR_REGISTRY = 'registry.cn-hangzhou.aliyuncs.com'
    PROD_HOST = '124.220.53.160'
    PROD_PORT = '22'
    PROD_USER = 'weavepress-deploy'
    PROD_SSH_CREDENTIALS_ID = 'weavepress-tencent-prod-ssh'
  }

  stages {
    stage('校验参数并检出精确 Server 版本') {
      steps {
        deleteDir()
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          [ "${BRANCH:-}" = master ] || { echo '生产构建只允许 master。' >&2; exit 64; }
          printf '%s' "${APP_SHA:-}" | grep -Eq '^[0-9a-f]{40}$' || {
            echo 'APP_SHA 必须是完整的 40 位小写 commit SHA。' >&2
            exit 64
          }
          printf '%s' "${TRIGGER_REPO:-}" | grep -Eq '^(manual|nas-hook|server-sync)$' || {
            echo 'TRIGGER_REPO 不在允许列表中。' >&2
            exit 64
          }
          export GIT_SSH_COMMAND="$NAS_GIT_SSH_COMMAND"
          git clone --no-checkout --branch master --single-branch "$NAS_REPO" source
          git -C source cat-file -e "$APP_SHA^{commit}"
          git -C source merge-base --is-ancestor "$APP_SHA" origin/master
          git -C source checkout --detach "$APP_SHA"
          actual_sha="$(git -C source rev-parse HEAD)"
          test "$actual_sha" = "$APP_SHA"
          test -f source/server/deployments/Dockerfile
          printf '%s\n' "$actual_sha" > APP_SHA_RESOLVED
        '''
        script {
          def resolved = readFile('APP_SHA_RESOLVED').trim()
          if (!(resolved ==~ /[0-9a-f]{40}/) || resolved != params.APP_SHA) {
            error('检出结果与 APP_SHA 不一致。')
          }
          currentBuild.displayName = "#${env.BUILD_NUMBER} Server ${resolved.take(8)}"
          currentBuild.description = "APP_SHA=${resolved} trigger=${params.TRIGGER_REPO}"
        }
      }
    }

    stage('构建 Server 三个不可变镜像') {
      steps {
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          docker build -f source/server/deployments/Dockerfile --build-arg TARGET=api -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA source/server
          docker build -f source/server/deployments/Dockerfile --build-arg TARGET=worker -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA source/server
          docker build -f source/server/deployments/Dockerfile --build-arg TARGET=migrate -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA source/server
          docker image inspect \
            "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA" \
            "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA" \
            "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA" >/dev/null
        '''
      }
    }

    stage('推送 Server 镜像到 ACR') {
      when {
        anyOf {
          expression { return params.PUSH_ACR == true }
          expression { return params.DEPLOY == true }
        }
      }
      steps {
        withCredentials([usernamePassword(credentialsId: 'aliyun-acr-zdzq', usernameVariable: 'ACR_USER', passwordVariable: 'ACR_PASSWORD')]) {
          sh(label: '推送三个不可变镜像（每个最多三次）', script: '''#!/usr/bin/env bash
            set -Eeuo pipefail
            set +x
            docker_config="$(mktemp -d)"
            export DOCKER_CONFIG="$docker_config"
            cleanup() {
              docker logout registry.cn-hangzhou.aliyuncs.com >/dev/null 2>&1 || true
              rm -rf -- "$docker_config"
            }
            trap cleanup EXIT
            printf '%s\n' "$ACR_PASSWORD" | docker login --username "$ACR_USER" --password-stdin registry.cn-hangzhou.aliyuncs.com >/dev/null
            ACR_PASSWORD=''
            push_image() {
              local image="$1"
              local attempt=1
              until docker push "$image"; do
                [ "$attempt" -lt 3 ] || return 1
                sleep $((attempt * 10))
                attempt=$((attempt + 1))
              done
            }
            push_image "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA"
            push_image "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA"
            push_image "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA"
          ''')
        }
      }
    }

    // The forced command returns success only after migration completion,
    // API healthy, and Server image SHA verification gates have passed.
    stage('发布并确认 migration completion、API healthy、Server image SHA') {
      when { expression { return params.DEPLOY == true } }
      steps {
        withCredentials([usernamePassword(credentialsId: 'aliyun-acr-zdzq', usernameVariable: 'ACR_USER', passwordVariable: 'ACR_PASSWORD')]) {
          sshagent(credentials: [env.PROD_SSH_CREDENTIALS_ID]) {
            sh '''#!/usr/bin/env bash
              set -Eeuo pipefail
              set +x
              printf '%s\n%s\n' "$ACR_USER" "$ACR_PASSWORD" | ssh \
                -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes \
                -o UserKnownHostsFile=/var/jenkins_home/.ssh/known_hosts \
                -o ServerAliveInterval=30 -o ServerAliveCountMax=30 \
                -p "$PROD_PORT" "$PROD_USER@$PROD_HOST" \
                "$APP_SHA --component=server"
            '''
          }
        }
      }
    }
  }

  post {
    always {
      deleteDir()
    }
  }
}
