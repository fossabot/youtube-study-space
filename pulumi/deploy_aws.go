package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/apigateway"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v4/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func deployAWS() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		
		customLambdaPolicy, err := iam.NewPolicy(ctx, "customLambdaPolicy", &iam.PolicyArgs{
			Policy: pulumi.String(`{
				"Version": "2012-10-17",
					"Statement": [
						{
							"Effect": "Allow",
							"Action": "logs:CreateLogGroup",
							"Resource": "arn:aws:logs:ap-northeast-1:652333062396:*"
						},
						{
							"Effect": "Allow",
							"Action": [
								"logs:CreateLogStream",
								"logs:PutLogEvents"
							],
							"Resource": [
								"arn:aws:logs:ap-northeast-1:652333062396:log-group:/aws/lambda/my-first-golang-lambda-function:*"
							]
						},
						{
							"Effect": "Allow",
							"Action": [
								"iam:PassRole"
							],
							"Resource": [
								"arn:aws:iam::652333062396:role/service-role/my-first-golang-lambda-function-role-cb8uw4th"
							]
						}
					]
			}`),
		})
		if err != nil {
			return err
		}
		
		lambdaRole, err := iam.NewRole(ctx, "my-first-golang-lambda-function-role-cb8uw4th", &iam.RoleArgs{
			AssumeRolePolicy: customLambdaPolicy,
		})
		if err != nil {
			return err
		}
		
		_, err = iam.NewRolePolicyAttachment(ctx, "customLambdaPolicyAttachment", &iam.RolePolicyAttachmentArgs{
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/PowerUserAccess"),
			Role:      lambdaRole.Name,
		})
		if err != nil {
			return err
		}
		
		// Create the Lambda function
		lambdaFunction, err := lambda.NewFunction(ctx, "youtube_organize_database", &lambda.FunctionArgs{
			Runtime:    pulumi.String("go1.x"),
			Code:       pulumi.NewFileArchive("../system/aws-lambda/youtube_organize_database/main.zip"),
			Timeout:    pulumi.Int(50),
			MemorySize: pulumi.Int(128),
			Handler:    pulumi.String("main"),
			Role:       lambdaRole.Arn,
		})
		if err != nil {
			return err
		}
		
		deployAPIGateway(lambdaFunction.InvokeArn)
		
		return nil
	})
}

func deployAPIGateway(lambdaFunctionArn pulumi.StringOutput, function *lambda.Function) {
	pulumi.Run(func(ctx *pulumi.Context) error {
		account, err := aws.GetCallerIdentity(ctx)
		if err != nil {
			return err
		}
		
		region, err := aws.GetRegion(ctx, &aws.GetRegionArgs{})
		if err != nil {
			return err
		}
		
		restApi, err := apigateway.NewRestApi(ctx, "youtube-study-space-rest-api", &apigateway.RestApiArgs{
			Name: pulumi.String("youtube-study-space-rest-api"),
		})
		if err != nil {
			return err
		}
		
		// Create a resource for the Lambda function
		apiGatewayResource, err := apigateway.NewResource(ctx, "set_desired_max_seats", &apigateway.ResourceArgs{
			ParentId: restApi.RootResourceId,
			PathPart: pulumi.String("set_desired_max_seats"),
			RestApi:  restApi.ID(),
		})
		if err != nil {
			return err
		}
		
		_, err = apigateway.NewMethod(ctx, "PostMethod", &apigateway.MethodArgs{
			HttpMethod:     pulumi.String("POST"),
			ResourceId:     apiGatewayResource.ID(),
			RestApi:        restApi.ID(),
			Authorization:  pulumi.String("NONE"),
			ApiKeyRequired: pulumi.Bool(true),
		})
		
		// Create an API Gateway integration with the Lambda function
		_, err = apigateway.NewIntegration(ctx, "LambdaIntegration", &apigateway.IntegrationArgs{
			IntegrationHttpMethod: pulumi.String("POST"),
			HttpMethod:            pulumi.String("POST"),
			Type:                  pulumi.String("AWS"),
			Uri:                   lambdaFunctionArn,
			ResourceId:            apiGatewayResource.ID(),
			RestApi:               restApi.ID(),
		})
		if err != nil {
			return err
		}
		
		// Add a resource based policy to the Lambda function.
		// This is the final step and allows AWS API Gateway to communicate with the AWS Lambda function
		permission, err := lambda.NewPermission(ctx, "APIPermission", &lambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  function.Name,
			Principal: pulumi.String("apigateway.amazonaws.com"),
			SourceArn: pulumi.Sprintf("arn:aws:execute-api:%s:%s:%s/*/*/*", region.Name, account.AccountId, restApi.ID()),
		}, pulumi.DependsOn([]pulumi.Resource{apiGatewayResource}))
		if err != nil {
			return err
		}
		
		// Create an API key to manage usage
		apiKey, err := apigateway.NewApiKey(ctx, "youtube-study-space-api-key", &apigateway.ApiKeyArgs{})
		if err != nil {
			return err
		}
		
		apiId := restApi.ID().ApplyT(func(id pulumi.IDOutput) pulumi.StringOutput {
			return id.ToStringOutput()
		}).ApplyT(func(id interface{}) string {
			return id.(string)
		}).(pulumi.StringOutput)
		stageName := restApi.Stage.ApplyT(func(stage *apigateway.Stage) pulumi.StringOutput {
			return stage.StageName
		}).ApplyT(func(stageName interface{}) string {
			return stageName.(string)
		}).(pulumi.StringOutput)
		// Define usage plan for an API stage
		usagePlan, err := apigateway.NewUsagePlan(ctx, "usage-plan", &apigateway.UsagePlanArgs{
			ApiStages: apigateway.UsagePlanApiStageArray{
				apigateway.UsagePlanApiStageArgs{
					ApiId: apiId,
					Stage: stageName,
				},
			},
		})
		if err != nil {
			return err
		}
		
		// Associate the key to the plan
		_, err = apigateway.NewUsagePlanKey(ctx, "usage-plan-key", &apigateway.UsagePlanKeyArgs{
			KeyId:       apiKey.ID(),
			KeyType:     pulumi.String("API_KEY"),
			UsagePlanId: usagePlan.ID(),
		})
		if err != nil {
			return err
		}
		
		ctx.Export("api-key-value", apiKey.Value)
		
		// Create a deployment of the API Gateway
		_, err = apigateway.NewDeployment(ctx, "exampleDeployment", &apigateway.DeploymentArgs{
			RestApi: restApi.ID(),
		}, pulumi.DependsOn([]pulumi.Resource{apiGatewayResource, function, permission}))
		if err != nil {
			return err
		}
		
		return nil
	})
}
