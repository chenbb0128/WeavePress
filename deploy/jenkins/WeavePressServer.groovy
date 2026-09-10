pipeline {
  agent any

  options {
    timestamps()
    timeout(time: 45, unit: 'MINUTES')
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
    PROD_SSH_CREDENTIALS_ID = 'yy-edusystem-tencent-prod-ssh'
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
          timeout 300 git clone --no-checkout --branch master --single-branch --no-tags "$NAS_REPO" source
          timeout 30 git -C source show 'refs/remotes/origin/master:deploy/jenkins/checkout-component-source.sh' > checkout-component-source.sh
          chmod 0700 checkout-component-source.sh
          timeout 240 bash checkout-component-source.sh source server "$APP_SHA" component-current.sh
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
      environment {
        HTTP_PROXY = 'http://192.168.31.227:7890'
        HTTPS_PROXY = 'http://192.168.31.227:7890'
        NO_PROXY = '127.0.0.1,localhost,192.168.31.240,124.220.53.160,116.62.159.237'
      }
      steps {
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          docker build -f source/server/deployments/Dockerfile --build-arg HTTP_PROXY="$HTTP_PROXY" --build-arg HTTPS_PROXY="$HTTPS_PROXY" --build-arg NO_PROXY="$NO_PROXY" --build-arg APP_SHA=$APP_SHA --build-arg TARGET=api -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA source/server
          docker build -f source/server/deployments/Dockerfile --build-arg HTTP_PROXY="$HTTP_PROXY" --build-arg HTTPS_PROXY="$HTTPS_PROXY" --build-arg NO_PROXY="$NO_PROXY" --build-arg APP_SHA=$APP_SHA --build-arg TARGET=worker -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA source/server
          docker build -f source/server/deployments/Dockerfile --build-arg HTTP_PROXY="$HTTP_PROXY" --build-arg HTTPS_PROXY="$HTTPS_PROXY" --build-arg NO_PROXY="$NO_PROXY" --build-arg APP_SHA=$APP_SHA --build-arg TARGET=migrate -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA source/server
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
        sh '''#!/usr/bin/env bash
          set -Eeuo pipefail
          timeout 120 bash source/deploy/jenkins/verify-acr-immutable-policy.sh \
            registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api \
            registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker \
            registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate
        '''
        withCredentials([usernamePassword(credentialsId: 'aliyun-acr-zdzq', usernameVariable: 'ACR_USER', passwordVariable: 'ACR_PASSWORD')]) {
          sh(label: '推送三个不可变镜像', script: '''#!/usr/bin/env bash
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
            mkdir -p manifest-digests
            bash source/deploy/jenkins/publish-immutable-image.sh \
              "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA" \
              "$APP_SHA" manifest-digests/api.digest
            bash source/deploy/jenkins/publish-immutable-image.sh \
              "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA" \
              "$APP_SHA" manifest-digests/worker.digest
            bash source/deploy/jenkins/publish-immutable-image.sh \
              "registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA" \
              "$APP_SHA" manifest-digests/migrate.digest
          ''')
        }
        archiveArtifacts artifacts: 'manifest-digests/*.digest', fingerprint: true
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
              export GIT_SSH_COMMAND="$NAS_GIT_SSH_COMMAND"
              bash component-current.sh source server "$APP_SHA"
              api_digest="$(<manifest-digests/api.digest)"
              worker_digest="$(<manifest-digests/worker.digest)"
              migrate_digest="$(<manifest-digests/migrate.digest)"
              for digest in "$api_digest" "$worker_digest" "$migrate_digest"; do
                printf '%s' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$'
              done
              printf '%s\n%s\n%s\n%s\n%s\n' \
                "$ACR_USER" "$ACR_PASSWORD" \
                "$api_digest" "$worker_digest" "$migrate_digest" | ssh \
                -o BatchMode=yes -o StrictHostKeyChecking=yes \
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
