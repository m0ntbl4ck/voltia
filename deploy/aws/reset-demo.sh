#!/usr/bin/env bash
# Puts the deployed demo back to "no analysis yet": deletes the analyses, the
# anomalies and the actions applied to them. Meters, readings, events, the demo
# user and the cached model explanations stay, so the next analysis is instant
# and still reads as written by the model.
#
# Usage: AWS_PROFILE=<profile of the account that runs the demo> ./reset-demo.sh
# Needs the AWS CLI and permission to use Session Manager on the instance.
set -euo pipefail

: "${AWS_PROFILE:?set AWS_PROFILE to the profile of the account that runs the demo}"
REGION="${AWS_REGION:-us-east-1}"
export AWS_PAGER=""

INSTANCE="$(aws ec2 describe-instances --region "$REGION" \
  --filters Name=tag:project,Values=voltia Name=instance-state-name,Values=running \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)"
if [ -z "$INSTANCE" ] || [ "$INSTANCE" = "None" ]; then
  echo "No running instance tagged project=voltia in $REGION" >&2
  exit 1
fi

COUNT='select (select count(*) from analysis_runs) as runs, (select count(*) from anomalies) as anomalies, (select count(*) from anomaly_actions) as actions, (select count(*) from explanation_cache) as cached'
PSQL='docker exec deploy-postgres-1 psql -U voltia -d voltia'

COMMAND="$(aws ssm send-command --region "$REGION" --instance-ids "$INSTANCE" \
  --document-name AWS-RunShellScript \
  --parameters "commands=[\"echo before:\",\"$PSQL -c \\\"$COUNT\\\"\",\"$PSQL -c \\\"delete from anomaly_actions; delete from anomalies; delete from analysis_runs\\\"\",\"echo after:\",\"$PSQL -c \\\"$COUNT\\\"\"]" \
  --query Command.CommandId --output text)"

aws ssm wait command-executed --region "$REGION" --command-id "$COMMAND" --instance-id "$INSTANCE"
aws ssm get-command-invocation --region "$REGION" --command-id "$COMMAND" --instance-id "$INSTANCE" \
  --query StandardOutputContent --output text
